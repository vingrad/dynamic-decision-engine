package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// Default models per provider, applied when none is configured.
const (
	defaultOpenAIModel   = "gpt-4o"
	defaultDeepSeekModel = "deepseek-chat"
	// deepSeekBaseURL is DeepSeek's OpenAI-compatible endpoint.
	deepSeekBaseURL = "https://api.deepseek.com"
)

// OpenAIConfig configures an OpenAI-compatible planner. The same adapter backs
// both OpenAI and DeepSeek (DeepSeek implements the OpenAI chat-completions API),
// so the provider only changes the name, default model, base URL and API key.
type OpenAIConfig struct {
	// Provider is "openai" or "deepseek"; it drives Name() and provenance.
	Provider  string
	APIKey    string // falls back to the SDK's env lookup (OPENAI_API_KEY) if empty
	Model     string // optional; defaults per provider
	BaseURL   string // optional; defaults to DeepSeek's endpoint when Provider=="deepseek"
	MaxTokens int64  // optional; defaults to 4096
}

// OpenAIPlanner is a real planner backed by any OpenAI-compatible chat API. It
// elicits a structured plan via a forced function call.
type OpenAIPlanner struct {
	client    openai.Client
	provider  string
	model     string
	maxTokens int64
}

// NewOpenAIPlanner constructs an OpenAI-compatible planner for the given provider.
func NewOpenAIPlanner(cfg OpenAIConfig) *OpenAIPlanner {
	provider := cfg.Provider
	if provider == "" {
		provider = "openai"
	}

	model := cfg.Model
	if model == "" {
		if provider == "deepseek" {
			model = defaultDeepSeekModel
		} else {
			model = defaultOpenAIModel
		}
	}

	baseURL := cfg.BaseURL
	if baseURL == "" && provider == "deepseek" {
		baseURL = deepSeekBaseURL
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	opts := []option.RequestOption{}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return &OpenAIPlanner{
		client:    openai.NewClient(opts...),
		provider:  provider,
		model:     model,
		maxTokens: maxTokens,
	}
}

// Name implements Planner; it returns the provider so provenance reflects which
// backend produced the plan.
func (p *OpenAIPlanner) Name() string { return p.provider }

// GeneratePlan implements Planner via a forced function call.
func (p *OpenAIPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	g := req.Goal
	if g.Objective == "" {
		return PlanResult{}, fmt.Errorf("llm: goal objective is required")
	}

	userPayload, err := planUserPayload(g, req.SignalNote)
	if err != nil {
		return PlanResult{}, err
	}

	properties, required := planSchema()
	params := openai.ChatCompletionNewParams{
		Model:               p.model,
		MaxCompletionTokens: openai.Int(p.maxTokens),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPayload),
		},
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name:        planToolName,
				Description: openai.String("Submit the ranked action plan for the given goal."),
				Parameters: shared.FunctionParameters{
					"type":       "object",
					"properties": properties,
					"required":   required,
				},
			}),
		},
		// Force the model to answer by calling submit_plan, guaranteeing structured output.
		ToolChoice: openai.ToolChoiceOptionFunctionToolChoice(
			openai.ChatCompletionNamedToolChoiceFunctionParam{Name: planToolName},
		),
	}

	start := time.Now()
	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return PlanResult{}, fmt.Errorf("llm: %s request: %w", p.provider, err)
	}
	latency := time.Since(start)

	dto, err := extractOpenAIPlan(resp)
	if err != nil {
		return PlanResult{}, err
	}
	if len(dto.RankedMoves) == 0 {
		return PlanResult{}, fmt.Errorf("llm: model returned no ranked moves")
	}

	promptVersion := p.provider + "-v1"
	prov := domain.DecisionProvenance{
		ReasoningSummary: dto.ReasoningSummary,
		InputSnapshotID:  inputSnapshotID(g, req.SignalNote),
		Planner:          p.provider,
		PromptVersion:    promptVersion,
		Model:            p.model,
	}

	return PlanResult{
		Summary:     dto.Summary,
		RankedMoves: mapMoves(dto),
		Provenance:  prov,
		Invocation: domain.ModelInvocation{
			Model:            p.model,
			PromptVersion:    promptVersion,
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
			LatencyMS:        latency.Milliseconds(),
		},
	}, nil
}

// extractOpenAIPlan pulls the forced function call's arguments and decodes them.
func extractOpenAIPlan(resp *openai.ChatCompletion) (planDTO, error) {
	if len(resp.Choices) == 0 {
		return planDTO{}, fmt.Errorf("llm: model returned no choices")
	}
	for _, call := range resp.Choices[0].Message.ToolCalls {
		if call.Function.Name == planToolName {
			var dto planDTO
			if err := json.Unmarshal([]byte(call.Function.Arguments), &dto); err != nil {
				return planDTO{}, fmt.Errorf("llm: decode tool arguments: %w", err)
			}
			return dto, nil
		}
	}
	return planDTO{}, fmt.Errorf("llm: model did not call %s", planToolName)
}

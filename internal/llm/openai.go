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

// VerifierName implements PlanVerifier.
func (p *OpenAIPlanner) VerifierName() string { return p.provider }

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
	// Domain packs inject prompt guidance via the GuidedPlanner; an empty override
	// leaves the base system prompt (and generic behaviour) unchanged.
	sys := effectiveSystemPrompt(systemPrompt, req.SystemPromptOverride)
	raw, inv, err := p.callStructured(ctx, sys, userPayload, planToolName, properties, required)
	if err != nil {
		return PlanResult{}, err
	}

	var dto planDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return PlanResult{}, fmt.Errorf("llm: decode tool arguments: %w", err)
	}
	if len(dto.RankedMoves) == 0 {
		return PlanResult{}, fmt.Errorf("llm: model returned no ranked moves")
	}

	return PlanResult{
		Summary:     dto.Summary,
		RankedMoves: mapMoves(dto),
		Provenance: domain.DecisionProvenance{
			ReasoningSummary: dto.ReasoningSummary,
			InputSnapshotID:  inputSnapshotID(g, req.SignalNote),
			Planner:          p.provider,
			PromptVersion:    p.provider + "-v1",
			Model:            p.model,
			Strategy:         "single",
		},
		Invocation: inv,
	}, nil
}

// VerifyPlan implements PlanVerifier by asking the model to review a proposed plan.
func (p *OpenAIPlanner) VerifyPlan(ctx context.Context, goal domain.Goal, proposed PlanResult) (Verdict, domain.ModelInvocation, error) {
	userPayload, err := verifyUserPayload(goal, proposed)
	if err != nil {
		return Verdict{}, domain.ModelInvocation{}, err
	}
	properties, required := verifySchema()
	raw, inv, err := p.callStructured(ctx, verifySystemPrompt, userPayload, verifyToolName, properties, required)
	if err != nil {
		return Verdict{}, domain.ModelInvocation{}, err
	}
	var v Verdict
	if err := json.Unmarshal(raw, &v); err != nil {
		return Verdict{}, domain.ModelInvocation{}, fmt.Errorf("llm: decode verdict: %w", err)
	}
	return v, inv, nil
}

// callStructured runs a forced function call and returns the raw arguments JSON
// plus invocation metadata. Shared by GeneratePlan and VerifyPlan.
func (p *OpenAIPlanner) callStructured(ctx context.Context, system, userJSON, toolName string, properties map[string]any, required []string) ([]byte, domain.ModelInvocation, error) {
	params := openai.ChatCompletionNewParams{
		Model:               p.model,
		MaxCompletionTokens: openai.Int(p.maxTokens),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(userJSON),
		},
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name:        toolName,
				Description: openai.String("Return the structured result by calling this function."),
				Parameters: shared.FunctionParameters{
					"type":       "object",
					"properties": properties,
					"required":   required,
				},
			}),
		},
		ToolChoice: openai.ToolChoiceOptionFunctionToolChoice(
			openai.ChatCompletionNamedToolChoiceFunctionParam{Name: toolName},
		),
	}

	start := time.Now()
	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, domain.ModelInvocation{}, fmt.Errorf("llm: %s request: %w", p.provider, err)
	}
	inv := domain.ModelInvocation{
		Model:            p.model,
		PromptVersion:    p.provider + "-v1",
		PromptTokens:     int(resp.Usage.PromptTokens),
		CompletionTokens: int(resp.Usage.CompletionTokens),
		TotalTokens:      int(resp.Usage.TotalTokens),
		LatencyMS:        time.Since(start).Milliseconds(),
	}
	if len(resp.Choices) == 0 {
		return nil, inv, fmt.Errorf("llm: model returned no choices")
	}
	for _, call := range resp.Choices[0].Message.ToolCalls {
		if call.Function.Name == toolName {
			return []byte(call.Function.Arguments), inv, nil
		}
	}
	return nil, inv, fmt.Errorf("llm: model did not call %s", toolName)
}

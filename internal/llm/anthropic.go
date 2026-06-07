package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// anthropicPromptVersion identifies the prompt/tooling contract used here. Bump
// it whenever the system prompt or tool schema changes materially.
const anthropicPromptVersion = "anthropic-v1"

// defaultAnthropicModel is used when no model is configured. Opus is the most
// capable tier and the right default for a reasoning-heavy planner.
// (anthropic.Model is an alias for string.)
const defaultAnthropicModel = anthropic.ModelClaudeOpus4_8

// AnthropicConfig configures the Anthropic-backed planner.
type AnthropicConfig struct {
	APIKey    string // optional; falls back to ANTHROPIC_API_KEY
	Model     string // optional; defaults to defaultAnthropicModel
	MaxTokens int64  // optional; defaults to 4096
}

// AnthropicPlanner is a real planner backed by Anthropic's Claude models. It
// elicits a structured plan via forced tool use, so the model must return data
// matching the engine's plan schema rather than free-form prose.
type AnthropicPlanner struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
}

// NewAnthropicPlanner constructs the planner. The API key is read from the config
// or, if empty, from the ANTHROPIC_API_KEY environment variable.
func NewAnthropicPlanner(cfg AnthropicConfig) *AnthropicPlanner {
	var opts []option.RequestOption
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	model := cfg.Model
	if model == "" {
		model = defaultAnthropicModel
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	return &AnthropicPlanner{
		client:    anthropic.NewClient(opts...),
		model:     model,
		maxTokens: maxTokens,
	}
}

// Name implements Planner.
func (*AnthropicPlanner) Name() string { return "anthropic" }

// GeneratePlan implements Planner by calling Claude with a forced tool call.
func (p *AnthropicPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	g := req.Goal
	if g.Objective == "" {
		return PlanResult{}, fmt.Errorf("llm: goal objective is required")
	}

	userPayload, err := planUserPayload(g, req.SignalNote)
	if err != nil {
		return PlanResult{}, err
	}

	start := time.Now()
	msg, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPayload)),
		},
		Tools: []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{
			Name:        planToolName,
			Description: anthropic.String("Submit the ranked action plan for the given goal."),
			InputSchema: anthropicInputSchema(),
		}}},
		// Force the model to answer by calling submit_plan, guaranteeing structured output.
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: planToolName},
		},
	})
	if err != nil {
		return PlanResult{}, fmt.Errorf("llm: anthropic request: %w", err)
	}
	latency := time.Since(start)

	dto, err := extractAnthropicPlan(msg)
	if err != nil {
		return PlanResult{}, err
	}
	if len(dto.RankedMoves) == 0 {
		return PlanResult{}, fmt.Errorf("llm: model returned no ranked moves")
	}

	prov := domain.DecisionProvenance{
		ReasoningSummary: dto.ReasoningSummary,
		InputSnapshotID:  inputSnapshotID(g, req.SignalNote),
		Planner:          "anthropic",
		PromptVersion:    anthropicPromptVersion,
		Model:            p.model,
	}

	return PlanResult{
		Summary:     dto.Summary,
		RankedMoves: mapMoves(dto),
		Provenance:  prov,
		Invocation: domain.ModelInvocation{
			Model:            p.model,
			PromptVersion:    anthropicPromptVersion,
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
			LatencyMS:        latency.Milliseconds(),
		},
	}, nil
}

// extractAnthropicPlan finds the forced tool-use block and decodes its input.
func extractAnthropicPlan(msg *anthropic.Message) (planDTO, error) {
	for _, block := range msg.Content {
		if tu, ok := block.AsAny().(anthropic.ToolUseBlock); ok && tu.Name == planToolName {
			var dto planDTO
			if err := json.Unmarshal([]byte(tu.Input), &dto); err != nil {
				return planDTO{}, fmt.Errorf("llm: decode tool input: %w", err)
			}
			return dto, nil
		}
	}
	return planDTO{}, fmt.Errorf("llm: model did not call %s", planToolName)
}

// anthropicInputSchema adapts the shared plan schema into the Anthropic tool type.
func anthropicInputSchema() anthropic.ToolInputSchemaParam {
	properties, required := planSchema()
	return anthropic.ToolInputSchemaParam{Properties: properties, Required: required}
}

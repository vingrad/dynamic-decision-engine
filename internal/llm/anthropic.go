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

// VerifierName implements PlanVerifier.
func (*AnthropicPlanner) VerifierName() string { return "anthropic" }

// GeneratePlan implements Planner by calling Claude with a forced tool call.
func (p *AnthropicPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	g := req.Goal
	if g.Objective == "" {
		return PlanResult{}, fmt.Errorf("llm: goal objective is required")
	}

	userPayload, err := planUserPayload(req)
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
		return PlanResult{}, fmt.Errorf("llm: decode tool input: %w", err)
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
			Planner:          "anthropic",
			PromptVersion:    anthropicPromptVersion,
			Model:            p.model,
			Strategy:         "single",
		},
		Invocation: inv,
	}, nil
}

// VerifyPlan implements PlanVerifier by asking Claude to review a proposed plan.
func (p *AnthropicPlanner) VerifyPlan(ctx context.Context, goal domain.Goal, proposed PlanResult) (Verdict, domain.ModelInvocation, error) {
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

// callStructured runs a forced-tool call and returns the raw tool input JSON plus
// invocation metadata. It is the shared primitive behind GeneratePlan and VerifyPlan.
func (p *AnthropicPlanner) callStructured(ctx context.Context, system, userJSON, toolName string, properties map[string]any, required []string) ([]byte, domain.ModelInvocation, error) {
	start := time.Now()
	msg, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userJSON)),
		},
		Tools: []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{
			Name:        toolName,
			Description: anthropic.String("Return the structured result by calling this tool."),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: properties, Required: required},
		}}},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: toolName},
		},
	})
	if err != nil {
		return nil, domain.ModelInvocation{}, fmt.Errorf("llm: anthropic request: %w", err)
	}
	inv := domain.ModelInvocation{
		Model:            p.model,
		PromptVersion:    anthropicPromptVersion,
		PromptTokens:     int(msg.Usage.InputTokens),
		CompletionTokens: int(msg.Usage.OutputTokens),
		TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
		LatencyMS:        time.Since(start).Milliseconds(),
	}
	for _, block := range msg.Content {
		if tu, ok := block.AsAny().(anthropic.ToolUseBlock); ok && tu.Name == toolName {
			return []byte(tu.Input), inv, nil
		}
	}
	return nil, inv, fmt.Errorf("llm: model did not call %s", toolName)
}

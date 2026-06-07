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
const defaultAnthropicModel = string(anthropic.ModelClaudeOpus4_8)

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
		model:     anthropic.Model(model),
		maxTokens: maxTokens,
	}
}

// Name implements Planner.
func (*AnthropicPlanner) Name() string { return "anthropic" }

// planToolName is the single tool the model is forced to call.
const planToolName = "submit_plan"

// planDTO mirrors the submit_plan tool schema; it is the wire shape returned by
// the model before mapping into domain types.
type planDTO struct {
	Summary          string    `json:"summary"`
	ReasoningSummary string    `json:"reasoning_summary"`
	RankedMoves      []moveDTO `json:"ranked_moves"`
}

type moveDTO struct {
	Title          string        `json:"title"`
	Description    string        `json:"description"`
	Confidence     float64       `json:"confidence"`
	ExpectedImpact string        `json:"expected_impact"`
	Effort         string        `json:"effort"`
	Risk           string        `json:"risk"`
	Rationale      string        `json:"rationale"`
	Experiment     experimentDTO `json:"experiment"`
	FallbackMoves  []string      `json:"fallback_moves"`
}

type experimentDTO struct {
	Title          string   `json:"title"`
	DurationDays   int      `json:"duration_days"`
	SuccessSignals []string `json:"success_signals"`
	KillCriteria   []string `json:"kill_criteria"`
}

// GeneratePlan implements Planner by calling Claude with a forced tool call.
func (p *AnthropicPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	g := req.Goal
	if g.Objective == "" {
		return PlanResult{}, fmt.Errorf("llm: goal objective is required")
	}

	userPayload, err := json.MarshalIndent(struct {
		Objective  string         `json:"objective"`
		Metric     string         `json:"metric,omitempty"`
		Target     string         `json:"target,omitempty"`
		Context    domain.Context `json:"context"`
		SignalNote string         `json:"new_signal,omitempty"`
	}{g.Objective, g.Metric, g.Target, g.Context, req.SignalNote}, "", "  ")
	if err != nil {
		return PlanResult{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	start := time.Now()
	msg, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		System: []anthropic.TextBlockParam{{
			Text: systemPrompt,
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(string(userPayload))),
		},
		Tools: []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{
			Name:        planToolName,
			Description: anthropic.String("Submit the ranked action plan for the given goal."),
			InputSchema: planInputSchema(),
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

	dto, err := extractPlan(msg)
	if err != nil {
		return PlanResult{}, err
	}
	if len(dto.RankedMoves) == 0 {
		return PlanResult{}, fmt.Errorf("llm: model returned no ranked moves")
	}

	moves := make([]domain.RankedMove, len(dto.RankedMoves))
	for i, m := range dto.RankedMoves {
		moves[i] = domain.RankedMove{
			Rank:           i + 1, // re-number to guarantee a clean 1..N ranking
			Title:          m.Title,
			Description:    m.Description,
			Confidence:     clampConfidence(m.Confidence),
			ExpectedImpact: level(m.ExpectedImpact),
			Effort:         level(m.Effort),
			Risk:           level(m.Risk),
			Rationale:      m.Rationale,
			Experiment: domain.Experiment{
				Title:          m.Experiment.Title,
				DurationDays:   m.Experiment.DurationDays,
				SuccessSignals: m.Experiment.SuccessSignals,
				KillCriteria:   m.Experiment.KillCriteria,
			},
			FallbackMoves: m.FallbackMoves,
		}
	}

	modelName := string(p.model)
	prov := domain.DecisionProvenance{
		ReasoningSummary: dto.ReasoningSummary,
		InputSnapshotID:  inputSnapshotID(g, req.SignalNote),
		Planner:          "anthropic",
		PromptVersion:    anthropicPromptVersion,
		Model:            modelName,
	}

	return PlanResult{
		Summary:     dto.Summary,
		RankedMoves: moves,
		Provenance:  prov,
		Invocation: domain.ModelInvocation{
			Model:            modelName,
			PromptVersion:    anthropicPromptVersion,
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
			LatencyMS:        latency.Milliseconds(),
		},
	}, nil
}

// extractPlan finds the forced tool-use block and decodes its input.
func extractPlan(msg *anthropic.Message) (planDTO, error) {
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

// level coerces a model-provided string into a valid Level, defaulting to medium.
func level(s string) domain.Level {
	l := domain.Level(s)
	if l.Valid() {
		return l
	}
	return domain.LevelMedium
}

// clampConfidence keeps confidence within [0, 1].
func clampConfidence(c float64) float64 {
	switch {
	case c < 0:
		return 0
	case c > 1:
		return 1
	default:
		return c
	}
}

const systemPrompt = `You are the reasoning core of a decision-planning engine.

Given a goal with its context, assets and constraints, produce a small set of
ranked action paths (moves) that move toward the goal. This is decision support,
not a promise of the single best answer.

For each move provide: a clear title and description; an honest confidence in
[0,1]; expected_impact, effort and risk each as exactly one of "low", "medium" or
"high"; a concise rationale; a first experiment with a duration in days, concrete
success signals and kill/pivot criteria; and one or more fallback moves.

Rank moves from strongest to weakest (the first is the top recommendation).
Prefer moves that exploit existing assets and de-risk binding constraints. Be
calibrated: do not inflate confidence. Return your answer by calling the
submit_plan tool — do not write prose outside the tool call.`

// planInputSchema returns the JSON schema for the submit_plan tool.
func planInputSchema() anthropic.ToolInputSchemaParam {
	levelEnum := map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}}
	strArray := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"summary":           map[string]any{"type": "string"},
			"reasoning_summary": map[string]any{"type": "string"},
			"ranked_moves": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":           map[string]any{"type": "string"},
						"description":     map[string]any{"type": "string"},
						"confidence":      map[string]any{"type": "number"},
						"expected_impact": levelEnum,
						"effort":          levelEnum,
						"risk":            levelEnum,
						"rationale":       map[string]any{"type": "string"},
						"experiment": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"title":           map[string]any{"type": "string"},
								"duration_days":   map[string]any{"type": "integer"},
								"success_signals": strArray,
								"kill_criteria":   strArray,
							},
							"required": []string{"title", "duration_days", "success_signals", "kill_criteria"},
						},
						"fallback_moves": strArray,
					},
					"required": []string{"title", "description", "confidence", "expected_impact", "effort", "risk", "rationale", "experiment"},
				},
			},
		},
		Required: []string{"summary", "reasoning_summary", "ranked_moves"},
	}
}

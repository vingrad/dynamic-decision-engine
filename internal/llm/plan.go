package llm

import (
	"encoding/json"
	"fmt"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// planToolName is the single tool/function a real planner is forced to call so
// its output is structured rather than free-form prose.
const planToolName = "submit_plan"

// systemPrompt frames the engine's job for any chat-style model.
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

// planDTO mirrors the submit_plan schema: the wire shape a model returns before
// it is mapped into domain types. Shared by every real planner.
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

// planUserPayload serialises the goal, context and any new signal into the JSON
// the model reasons over.
func planUserPayload(g domain.Goal, signalNote string) (string, error) {
	b, err := json.MarshalIndent(struct {
		Objective  string         `json:"objective"`
		Metric     string         `json:"metric,omitempty"`
		Target     string         `json:"target,omitempty"`
		Context    domain.Context `json:"context"`
		SignalNote string         `json:"new_signal,omitempty"`
	}{g.Objective, g.Metric, g.Target, g.Context, signalNote}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}
	return string(b), nil
}

// mapMoves converts the model's moves into domain RankedMoves, re-numbering ranks
// 1..N and coercing enums/confidence into valid ranges.
func mapMoves(dto planDTO) []domain.RankedMove {
	moves := make([]domain.RankedMove, len(dto.RankedMoves))
	for i, m := range dto.RankedMoves {
		moves[i] = domain.RankedMove{
			Rank:           i + 1,
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
	return moves
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

// planSchema returns the JSON schema for the submit_plan tool as a plain map of
// properties plus the list of required fields, so each SDK can wrap it in its own
// tool/function parameter type.
func planSchema() (properties map[string]any, required []string) {
	levelEnum := map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}}
	strArray := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	properties = map[string]any{
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
	}
	required = []string{"summary", "reasoning_summary", "ranked_moves"}
	return properties, required
}

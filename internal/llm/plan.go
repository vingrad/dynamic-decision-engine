package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

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
calibrated: do not inflate confidence.

Give each move a short, stable "key": a lowercase slug naming the underlying
action (e.g. "expand-paid-search"). When the input includes a "current_plan",
reuse a move's existing key whenever your move is the same underlying action,
even if you reword its title; mint a new key only for a genuinely new move. The
key is identity; the title is display.

Express how moves relate in execution. Use "depends_on" to list the keys of moves
that must finish before a move can start; only reference keys you also emit, and
never create a cycle — the moves must form a directed acyclic graph. Leave
"depends_on" empty for moves that can start immediately. Dependencies are about
ordering, not priority: a strongly ranked move may still depend on a weaker one.
Optionally label moves meant to run together with the same "parallel_group" (a
short tag like "discover" or "commit"); it is a grouping hint only.

Return your answer by calling the submit_plan tool — do not write prose outside
the tool call.`

// planDTO mirrors the submit_plan schema: the wire shape a model returns before
// it is mapped into domain types. Shared by every real planner.
type planDTO struct {
	Summary          string    `json:"summary"`
	ReasoningSummary string    `json:"reasoning_summary"`
	RankedMoves      []moveDTO `json:"ranked_moves"`
}

type moveDTO struct {
	Key            string        `json:"key"`
	Title          string        `json:"title"`
	Description    string        `json:"description"`
	Confidence     float64       `json:"confidence"`
	ExpectedImpact string        `json:"expected_impact"`
	Effort         string        `json:"effort"`
	Risk           string        `json:"risk"`
	Rationale      string        `json:"rationale"`
	Experiment     experimentDTO `json:"experiment"`
	FallbackMoves  []string      `json:"fallback_moves"`
	DependsOn      []string      `json:"depends_on"`
	ParallelGroup  string        `json:"parallel_group"`
}

type experimentDTO struct {
	Title          string   `json:"title"`
	DurationDays   int      `json:"duration_days"`
	SuccessSignals []string `json:"success_signals"`
	KillCriteria   []string `json:"kill_criteria"`
}

// currentMove is the compact view of a prior plan's move handed to the model on a
// replan, so it can preserve a move's stable key across rewordings.
type currentMove struct {
	Key   string `json:"key,omitempty"`
	Title string `json:"title"`
}

// planUserPayload serialises the goal, context and any new signal into the JSON
// the model reasons over. On a replan it also includes the structured signal
// payload (so the model reasons over the numbers, not just the note) and the
// current plan's moves (so it can reuse their keys for unchanged moves).
func planUserPayload(req PlanRequest) (string, error) {
	g := req.Goal
	var current []currentMove
	for _, m := range req.CurrentMoves {
		current = append(current, currentMove{Key: m.Key, Title: m.Title})
	}
	b, err := json.MarshalIndent(struct {
		Objective   string         `json:"objective"`
		Metric      string         `json:"metric,omitempty"`
		Target      string         `json:"target,omitempty"`
		Context     domain.Context `json:"context"`
		SignalNote  string         `json:"new_signal,omitempty"`
		SignalKind  string         `json:"new_signal_kind,omitempty"`
		SignalData  map[string]any `json:"new_signal_data,omitempty"`
		CurrentPlan []currentMove  `json:"current_plan,omitempty"`
	}{g.Objective, g.Metric, g.Target, g.Context, req.SignalNote, req.SignalKind, req.SignalPayload, current}, "", "  ")
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
			Key:            moveKeyOrSlug(m.Key, m.Title),
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
			DependsOn:     m.DependsOn,
			ParallelGroup: m.ParallelGroup,
		}
	}
	return moves
}

// moveKeyOrSlug returns the model-supplied key when present, otherwise a
// slug-normalised title, so every move carries a stable identity even when the
// model omits an explicit key.
func moveKeyOrSlug(key, title string) string {
	if key != "" {
		return key
	}
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen && b.Len() > 0 {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
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
					"key":             map[string]any{"type": "string"},
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
					"depends_on":     strArray,
					"parallel_group": map[string]any{"type": "string"},
				},
				"required": []string{"title", "description", "confidence", "expected_impact", "effort", "risk", "rationale", "experiment"},
			},
		},
	}
	required = []string{"summary", "reasoning_summary", "ranked_moves"}
	return properties, required
}

// --- Verification (cross-model review) --------------------------------------

// verifyToolName is the tool a verifier model is forced to call.
const verifyToolName = "review_plan"

// verifySystemPrompt frames the reviewer's job: critique, don't rewrite.
const verifySystemPrompt = `You are an independent reviewer of a proposed decision plan.

You did NOT write this plan. Critically assess each ranked move for: factual
support in the given context (flag claims that assume assets or facts not present),
realistic confidence calibration, and whether the move actually advances the goal.

For each move return a verdict: keep (true/false), an optional adjusted_confidence
in [0,1] when the proposer's confidence is mis-calibrated, and any concrete issues.
Be willing to drop weak or unsupported moves. Return your review by calling the
review_plan tool — do not write prose outside the tool call.`

// Verdict is a verifier's review of a proposed plan.
type Verdict struct {
	OverallNote string        `json:"overall_note"`
	Moves       []MoveVerdict `json:"moves"`
}

// MoveVerdict is the review of a single move, matched to the proposal by Title.
type MoveVerdict struct {
	Title              string   `json:"title"`
	Keep               bool     `json:"keep"`
	AdjustedConfidence *float64 `json:"adjusted_confidence"`
	Issues             []string `json:"issues"`
}

// verifyUserPayload serialises the goal and the proposed moves for review.
func verifyUserPayload(g domain.Goal, proposed PlanResult) (string, error) {
	type reviewMove struct {
		Title          string  `json:"title"`
		Description    string  `json:"description"`
		Confidence     float64 `json:"confidence"`
		ExpectedImpact string  `json:"expected_impact"`
		Effort         string  `json:"effort"`
		Risk           string  `json:"risk"`
		Rationale      string  `json:"rationale"`
	}
	moves := make([]reviewMove, len(proposed.RankedMoves))
	for i, m := range proposed.RankedMoves {
		moves[i] = reviewMove{
			Title: m.Title, Description: m.Description, Confidence: m.Confidence,
			ExpectedImpact: string(m.ExpectedImpact), Effort: string(m.Effort),
			Risk: string(m.Risk), Rationale: m.Rationale,
		}
	}
	b, err := json.MarshalIndent(struct {
		Objective    string         `json:"objective"`
		Metric       string         `json:"metric,omitempty"`
		Target       string         `json:"target,omitempty"`
		Context      domain.Context `json:"context"`
		ProposedPlan []reviewMove   `json:"proposed_plan"`
	}{g.Objective, g.Metric, g.Target, g.Context, moves}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("llm: marshal verify request: %w", err)
	}
	return string(b), nil
}

// verifySchema returns the JSON schema for the review_plan tool.
func verifySchema() (properties map[string]any, required []string) {
	properties = map[string]any{
		"overall_note": map[string]any{"type": "string"},
		"moves": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":               map[string]any{"type": "string"},
					"keep":                map[string]any{"type": "boolean"},
					"adjusted_confidence": map[string]any{"type": "number"},
					"issues":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"title", "keep"},
			},
		},
	}
	required = []string{"overall_note", "moves"}
	return properties, required
}

// applyVerdict mutates a proposer's result according to a verifier's verdict:
// drops moves marked keep=false, applies adjusted confidence, collects issues,
// and re-numbers the surviving moves 1..N.
func applyVerdict(result PlanResult, v Verdict) (PlanResult, []string) {
	byTitle := make(map[string]MoveVerdict, len(v.Moves))
	for _, mv := range v.Moves {
		byTitle[mv.Title] = mv
	}
	var kept []domain.RankedMove
	var issues []string
	for _, m := range result.RankedMoves {
		mv, reviewed := byTitle[m.Title]
		if reviewed && !mv.Keep {
			issues = append(issues, "dropped \""+m.Title+"\": "+joinIssues(mv.Issues))
			continue
		}
		if reviewed && mv.AdjustedConfidence != nil {
			m.Confidence = clampConfidence(*mv.AdjustedConfidence)
		}
		if reviewed && len(mv.Issues) > 0 {
			issues = append(issues, "\""+m.Title+"\": "+joinIssues(mv.Issues))
		}
		m.Rank = len(kept) + 1
		kept = append(kept, m)
	}
	result.RankedMoves = kept
	return result, issues
}

func joinIssues(issues []string) string {
	if len(issues) == 0 {
		return "no detail"
	}
	out := issues[0]
	for _, s := range issues[1:] {
		out += "; " + s
	}
	return out
}

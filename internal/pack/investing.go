package pack

import (
	"github.com/vingrad/dynamic-decision-engine/internal/finance"
)

// investingPromptTemplate is appended to the base system prompt for investing
// goals. It reframes "moves" as falsifiable investment theses and — critically —
// requires an educational, not-financial-advice disclaimer in the plan summary.
const investingPromptTemplate = `DOMAIN: INVESTING

Treat each move as a falsifiable investment thesis, not a tip. For every move:
- State the thesis in one sentence: the specific, checkable claim about the asset.
- Give entry and exit conditions (price, valuation, or event triggers). Keep them
  qualitative here; precise numeric levels are produced by a separate scorer.
- Express conviction as a relative position size (starter / half / full), never as
  a dollar amount, and keep it consistent with the confidence you report.
- Make kill_criteria concrete thesis-invalidation events — the facts that, if true,
  prove the thesis wrong — not merely "the price went down".
- State a time horizon (e.g. weeks, quarters, years) and the dominant risk
  (valuation, execution, macro, liquidity).

Confidence must read as a calibrated probability that the thesis plays out over the
stated horizon. Do not inflate it; uncertainty is normal and expected.

You MUST include verbatim in the plan summary: "Educational decision-support only.
Not financial advice. Not a recommendation to buy or sell any security."`

// investingPack adds thesis-oriented prompting, a tighter materiality threshold
// (calibration matters more for investing), and default scoring tunables consumed
// by the optional numeric finance planner.
func investingPack() Descriptor {
	scoring := finance.DefaultScoringConfig()
	return Descriptor{
		ID:             "investing",
		Name:           "Investing",
		Version:        "1",
		PromptVersion:  "investing-v1",
		PromptTemplate: investingPromptTemplate,
		PlannerKind:    "finance",
		Eval:           EvaluatorConfig{ConfidenceDelta: 0.05},
		Scoring:        &scoring,
		Vocab: Vocabulary{
			AssetKinds:      []string{"capital", "edge", "information", "conviction", "liquidity", "time_horizon"},
			ConstraintKinds: []string{"risk_tolerance", "liquidity", "time_horizon", "drawdown_limit", "mandate", "tax"},
			SignalKinds:     []string{"price_move", "earnings", "macro", "valuation_change", "thesis_break"},
		},
		Validation: Validation{Rules: []ValidationRule{
			{
				Check:    CheckRequireAnyKind,
				Kinds:    []string{"risk_tolerance", "drawdown_limit"},
				Scopes:   []KindScope{ScopeConstraint},
				Field:    "context.constraints",
				Message:  "no risk_tolerance or drawdown_limit constraint; sizing will fall back to conservative defaults",
				Severity: SeverityWarning,
			},
			{
				Check:    CheckRequireAnyKind,
				Kinds:    []string{"time_horizon"},
				Scopes:   []KindScope{ScopeAsset, ScopeConstraint},
				Field:    "context",
				Message:  "no time_horizon given; thesis horizon-fit cannot be assessed",
				Severity: SeverityWarning,
			},
		}},
	}
}

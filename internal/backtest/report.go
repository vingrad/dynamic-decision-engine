package backtest

import (
	"fmt"
	"io"
	"time"
)

// Decision records one step of the replay.
type Decision struct {
	At            time.Time `json:"at"`
	Kind          string    `json:"kind"`
	Material      bool      `json:"material"`
	Reason        string    `json:"reason"`
	TopMove       string    `json:"top_move"`
	TopConfidence float64   `json:"top_confidence"`
	// TopRawConfidence is the top move's pre-calibration confidence (equal to
	// TopConfidence when no curve is installed). Calibration fitting and
	// evaluation always operate on this value, so the fit domain matches the
	// domain a freshly installed curve is applied to.
	TopRawConfidence float64 `json:"top_raw_confidence,omitempty"`
	ShouldKill       bool    `json:"should_kill"`
	// ForwardReturn is the top thesis ticker's return from decision time to the
	// end of the scenario (evaluation-only attribution; never a planner input).
	ForwardReturn float64 `json:"forward_return"`
	// Label is the realized outcome the Brier score compares confidence against:
	// 1 when the top thesis's forward return was positive (falling back to the
	// analyst kill label), else 0. Evaluation-only.
	Label float64 `json:"label"`
	// SelectedStrategy is the strategy lens that won this decision's candidate
	// plan (Provenance.SelectedStrategy); empty when no selector is in play.
	SelectedStrategy string `json:"selected_strategy,omitempty"`
}

// Report summarises a backtest run. The metrics describe decision/replanning
// quality, not a tradeable strategy return (see package doc).
type Report struct {
	Scenario        string     `json:"scenario"`
	Decisions       []Decision `json:"decisions"`
	VersionsCreated int        `json:"versions_created"`
	KillPrecision   float64    `json:"kill_precision"` // correct reactions / total reactions
	KillRecall      float64    `json:"kill_recall"`    // correct reactions / total should-kill events
	// BrierScore is the mean squared error between each decision's top-move
	// confidence and its realized outcome label (1 if the top thesis's forward
	// return was positive, falling back to the analyst kill label). Lower is
	// better; a coin-flip confidence of 0.5 scores 0.25.
	BrierScore float64 `json:"brier_score"`
	// NoiseRobustness is the share of non-kill events that did NOT trigger a
	// material replan (1 == never reacted to noise).
	NoiseRobustness float64 `json:"noise_robustness"`
	HypotheticalPnL float64 `json:"hypothetical_pnl"`
	// StrategyShare counts decisions per winning strategy (selector runs only),
	// making winner flapping visible at a glance.
	StrategyShare map[string]int `json:"strategy_share,omitempty"`
	// StrategyFlips counts decisions whose winning strategy differed from the
	// previous decision's — the flapping metric the backtest gates bound.
	StrategyFlips int `json:"strategy_flips,omitempty"`
}

// Render writes a human-readable report to w.
func (r Report) Render(w io.Writer) {
	fmt.Fprintf(w, "Backtest: %s\n", r.Scenario)
	fmt.Fprintf(w, "  versions created : %d\n", r.VersionsCreated)
	fmt.Fprintf(w, "  kill precision   : %.2f\n", r.KillPrecision)
	fmt.Fprintf(w, "  kill recall      : %.2f\n", r.KillRecall)
	fmt.Fprintf(w, "  brier score      : %.3f (lower is better; 0.25 == coin-flip confidence)\n", r.BrierScore)
	fmt.Fprintf(w, "  noise robustness : %.2f (share of non-kill events not reacted to)\n", r.NoiseRobustness)
	fmt.Fprintf(w, "  hypothetical pnl : %+.2f%% (illustrative, not a strategy return)\n", r.HypotheticalPnL*100)
	fmt.Fprintln(w, "  decisions:")
	for _, d := range r.Decisions {
		mark := " "
		if d.Material {
			mark = "*"
		}
		fmt.Fprintf(w, "    %s %s [%s] material=%v should_kill=%v top=%q conf=%.2f fwd=%+.2f%% — %s\n",
			mark, d.At.Format("2006-01-02"), d.Kind, d.Material, d.ShouldKill, d.TopMove, d.TopConfidence, d.ForwardReturn*100, d.Reason)
	}
}

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
	ShouldKill    bool      `json:"should_kill"`
}

// Report summarises a backtest run. The metrics describe decision/replanning
// quality, not a tradeable strategy return (see package doc).
type Report struct {
	Scenario        string     `json:"scenario"`
	Decisions       []Decision `json:"decisions"`
	VersionsCreated int        `json:"versions_created"`
	KillPrecision   float64    `json:"kill_precision"` // correct reactions / total reactions
	KillRecall      float64    `json:"kill_recall"`    // correct reactions / total should-kill events
	HypotheticalPnL float64    `json:"hypothetical_pnl"`
}

// Render writes a human-readable report to w.
func (r Report) Render(w io.Writer) {
	fmt.Fprintf(w, "Backtest: %s\n", r.Scenario)
	fmt.Fprintf(w, "  versions created : %d\n", r.VersionsCreated)
	fmt.Fprintf(w, "  kill precision   : %.2f\n", r.KillPrecision)
	fmt.Fprintf(w, "  kill recall      : %.2f\n", r.KillRecall)
	fmt.Fprintf(w, "  hypothetical pnl : %+.2f%% (illustrative, not a strategy return)\n", r.HypotheticalPnL*100)
	fmt.Fprintln(w, "  decisions:")
	for _, d := range r.Decisions {
		mark := " "
		if d.Material {
			mark = "*"
		}
		fmt.Fprintf(w, "    %s %s [%s] material=%v should_kill=%v top=%q conf=%.2f — %s\n",
			mark, d.At.Format("2006-01-02"), d.Kind, d.Material, d.ShouldKill, d.TopMove, d.TopConfidence, d.Reason)
	}
}

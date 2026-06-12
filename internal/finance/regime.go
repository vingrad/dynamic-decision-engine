package finance

import (
	"fmt"
	"math"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

// The market regime is a COARSE DESCRIPTIVE LABEL of recent price action, only
// ever known in hindsight. It gates which strategy lenses compete for a goal;
// it does not forecast and must never be read as market timing. Per the
// package doc's honesty framing, RegimeUnknown is a first-class answer — too
// little history, or nothing distinctive about the tape — and an unknown
// regime gates nothing.

// Regime labels recent price action.
type Regime string

const (
	RegimeTrend   Regime = "trend"
	RegimeRange   Regime = "range"
	RegimeHighVol Regime = "high_vol"
	RegimeUnknown Regime = ""
)

const (
	// regimeMinBars refuses to label fewer than ~2 months of daily bars (or ~9
	// months of weekly bars) — below that the label would be noise.
	regimeMinBars = 40
	// regimeWindowBars classifies on the trailing window only, so an old crash
	// cannot keep the label stuck on high_vol forever.
	regimeWindowBars = 126
	// regimeHighVolMonthly: a 21-day scaled volatility at or above 12% reads as
	// a high-volatility tape regardless of direction.
	regimeHighVolMonthly = 0.12
	// regimeHighVolDrawdown: a 20% peak-to-trough fall inside the window reads
	// as high_vol even when day-to-day volatility looks tame (slow bleeds).
	regimeHighVolDrawdown = 0.20
	// regimeTrendER is the Kaufman efficiency-ratio threshold for a trend: net
	// movement at least 30% of total movement.
	regimeTrendER = 0.30
)

// RegimeReading is one classification with the numbers behind it, so the
// label is auditable.
type RegimeReading struct {
	Regime        Regime  `json:"regime"`
	TrendStrength float64 `json:"trend_strength"` // efficiency ratio in [0, 1]
	MonthlyVol    float64 `json:"monthly_vol"`    // 21-day scaled volatility
	Note          string  `json:"note"`
}

// ClassifyRegime labels the trailing window of a bar series. It is pure and
// deterministic: same bars, same label. The high-volatility check runs first —
// risk dominates direction — then trend vs. range by efficiency ratio.
func ClassifyRegime(bars []marketdata.Bar) RegimeReading {
	if len(bars) < regimeMinBars {
		return RegimeReading{Regime: RegimeUnknown, Note: fmt.Sprintf("only %d bars; too little history to classify", len(bars))}
	}
	window := bars
	if len(window) > regimeWindowBars {
		window = window[len(window)-regimeWindowBars:]
	}

	rets := Returns(window)
	vol := Volatility(rets)
	monthlyVol := ScaledVol(vol, BarIntervalDays(window), 21)
	dd := MaxDrawdown(window)
	er := efficiencyRatio(window)

	r := RegimeReading{TrendStrength: er, MonthlyVol: monthlyVol}
	switch {
	case monthlyVol >= regimeHighVolMonthly || dd >= regimeHighVolDrawdown:
		r.Regime = RegimeHighVol
		r.Note = fmt.Sprintf("monthly vol %.1f%%, window drawdown %.1f%%", monthlyVol*100, dd*100)
	case er >= regimeTrendER:
		r.Regime = RegimeTrend
		r.Note = fmt.Sprintf("efficiency ratio %.2f over %d bars", er, len(window))
	default:
		r.Regime = RegimeRange
		r.Note = fmt.Sprintf("efficiency ratio %.2f over %d bars", er, len(window))
	}
	return r
}

// efficiencyRatio is Kaufman's signal-to-noise measure: net close-to-close
// movement over total close-to-close movement, in [0, 1]. A zero denominator
// (a perfectly flat series) reads as 0 — no trend.
func efficiencyRatio(bars []marketdata.Bar) float64 {
	if len(bars) < 2 {
		return 0
	}
	var total float64
	for i := 1; i < len(bars); i++ {
		total += math.Abs(bars[i].Close - bars[i-1].Close)
	}
	if total == 0 {
		return 0
	}
	return math.Abs(bars[len(bars)-1].Close-bars[0].Close) / total
}

// CombineRegimes folds per-ticker readings into one goal-level label: any
// high_vol wins (risk dominates), else the strict majority of trend vs. range;
// ties, empty input and all-unknown read as unknown.
func CombineRegimes(readings []RegimeReading) Regime {
	trend, rng := 0, 0
	for _, r := range readings {
		switch r.Regime {
		case RegimeHighVol:
			return RegimeHighVol
		case RegimeTrend:
			trend++
		case RegimeRange:
			rng++
		}
	}
	switch {
	case trend > rng:
		return RegimeTrend
	case rng > trend:
		return RegimeRange
	default:
		return RegimeUnknown
	}
}

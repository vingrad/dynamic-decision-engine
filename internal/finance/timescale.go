package finance

import (
	"math"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

// Holding windows (days) for deriving a stop distance from volatility. The cap
// keeps stops meaningful on multi-year goals: beyond a quarter, a "stop" sized
// by sqrt-time vol stops being a risk control.
const (
	DefaultHoldingDays = 30
	MaxStopHoldingDays = 90
)

// BarIntervalDays estimates the average spacing of a chronological bar series in
// days, so per-bar volatility can be projected across calendar windows without
// assuming a cadence. Returns 0 when the series has fewer than two bars.
func BarIntervalDays(bars []marketdata.Bar) float64 {
	if len(bars) < 2 {
		return 0
	}
	span := bars[len(bars)-1].Date.Sub(bars[0].Date).Hours() / 24
	if span <= 0 {
		return 0
	}
	return span / float64(len(bars)-1)
}

// ScaledVol projects per-bar volatility onto a holding window by sqrt-time:
// vol_bar * sqrt(holdingDays / barIntervalDays). It returns the per-bar vol
// unchanged when either day count is unknown, so callers degrade gracefully.
func ScaledVol(volPerBar, barIntervalDays float64, holdingDays int) float64 {
	if volPerBar <= 0 || barIntervalDays <= 0 || holdingDays <= 0 {
		return volPerBar
	}
	return volPerBar * math.Sqrt(float64(holdingDays)/barIntervalDays)
}

// VolImpliedHorizonDays estimates how many days a random walk with the given
// per-bar volatility needs to traverse targetFrac: (target/dailyVol)^2. This is
// a crossing-time heuristic, not a forecast. Returns 0 when the volatility or
// target is unknown.
func VolImpliedHorizonDays(targetFrac, volPerBar, barIntervalDays float64) int {
	if targetFrac <= 0 || volPerBar <= 0 || barIntervalDays <= 0 {
		return 0
	}
	dailyVol := volPerBar / math.Sqrt(barIntervalDays)
	d := targetFrac / dailyVol
	return int(math.Round(d * d))
}

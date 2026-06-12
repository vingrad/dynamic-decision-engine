package finance

import (
	"sort"

	"github.com/vingrad/dynamic-decision-engine/internal/strategy"
)

// Outcome-weighted strategy selection, the strategy analogue of calibrate.go:
// recorded outcomes are folded into per-strategy utility multipliers that the
// selector applies when lenses compete. Like the calibration curve, an empty
// result never changes behaviour, shrinkage keeps thin evidence near the
// identity, and the fit is deterministic — same samples, same weights.

// StrategySample is one decisive recorded outcome attributed to the strategy
// that won the decision, with the market regime in effect at plan time
// (RegimeUnknown when none was recorded).
type StrategySample struct {
	Strategy string `json:"strategy" yaml:"strategy"`
	Regime   Regime `json:"regime" yaml:"regime"`
	Success  bool   `json:"success" yaml:"success"`
	// Comparator is the comparison mode the decision ran under ("" reads as
	// "utility", the mode of every pre-comparator plan). Weights must only be
	// fitted from samples under ONE comparator — `dde strategy-fit` filters on
	// the domain's configured mode before calling FitStrategyWeights.
	Comparator string `json:"comparator,omitempty" yaml:"comparator,omitempty"`
}

const (
	// strategyFitMinSamples: buckets with fewer decisive outcomes are omitted
	// (identity weight) — a handful of trades is not evidence.
	strategyFitMinSamples = 5
	// strategyFitFloor / strategyFitCeil clamp the fitted multiplier so even a
	// perfect or abysmal record only tilts selection, never dictates it.
	strategyFitFloor = 0.5
	strategyFitCeil  = 1.5
)

// FitStrategyWeights fits selection weights from decisive outcomes. For every
// strategy it fits a pooled bucket (key "id") and, where the regime was
// recorded, a per-regime bucket (key "id@regime" — the selector resolves the
// regime-specific key first). Each bucket's weight is a Laplace-shrunk success
// rate mapped so that a 50% record is exactly 1.0 (no-op):
//
//	p̂ = (successes + 1) / (n + 2)
//	weight = clamp(2·p̂, 0.5, 1.5)
//
// Buckets below the minimum sample count are omitted entirely, which the
// selector reads as weight 1.0.
func FitStrategyWeights(samples []StrategySample) map[string]float64 {
	type bucket struct{ n, wins int }
	buckets := map[string]*bucket{}
	add := func(key string, success bool) {
		b, ok := buckets[key]
		if !ok {
			b = &bucket{}
			buckets[key] = b
		}
		b.n++
		if success {
			b.wins++
		}
	}
	for _, s := range samples {
		if s.Strategy == "" {
			continue
		}
		add(strategy.WeightKey(s.Strategy, ""), s.Success)
		if s.Regime != RegimeUnknown {
			add(strategy.WeightKey(s.Strategy, string(s.Regime)), s.Success)
		}
	}

	// Sorted key iteration keeps any float work order-independent and makes
	// rendered snippets stable.
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := map[string]float64{}
	for _, k := range keys {
		b := buckets[k]
		if b.n < strategyFitMinSamples {
			continue
		}
		p := (float64(b.wins) + 1) / (float64(b.n) + 2)
		out[k] = clampRange(2*p, strategyFitFloor, strategyFitCeil)
	}
	return out
}

package finance

import (
	"reflect"
	"testing"
)

func strategySamples(strategy string, regime Regime, wins, losses int) []StrategySample {
	var out []StrategySample
	for i := 0; i < wins; i++ {
		out = append(out, StrategySample{Strategy: strategy, Regime: regime, Success: true})
	}
	for i := 0; i < losses; i++ {
		out = append(out, StrategySample{Strategy: strategy, Regime: regime, Success: false})
	}
	return out
}

func TestFitStrategyWeights(t *testing.T) {
	t.Run("empty samples yield an empty (identity) table", func(t *testing.T) {
		if got := FitStrategyWeights(nil); len(got) != 0 {
			t.Errorf("expected empty table, got %v", got)
		}
	})

	t.Run("thin buckets are omitted", func(t *testing.T) {
		got := FitStrategyWeights(strategySamples("momentum", RegimeTrend, 4, 0))
		if len(got) != 0 {
			t.Errorf("4 samples must not produce a weight, got %v", got)
		}
	})

	t.Run("shrinkage arithmetic at known counts", func(t *testing.T) {
		// 5 wins, 0 losses: p̂ = 6/7, weight = 12/7 → clamped to 1.5.
		got := FitStrategyWeights(strategySamples("momentum", RegimeUnknown, 5, 0))
		if got["momentum"] != 1.5 {
			t.Errorf("perfect record must clamp to 1.5, got %v", got["momentum"])
		}
		// 0 wins, 5 losses: p̂ = 1/7, weight = 2/7 → clamped to 0.5.
		got = FitStrategyWeights(strategySamples("value", RegimeUnknown, 0, 5))
		if got["value"] != 0.5 {
			t.Errorf("losing record must clamp to 0.5, got %v", got["value"])
		}
		// 3 wins, 3 losses: p̂ = 4/8 = 0.5 → weight exactly 1.0 (no-op).
		got = FitStrategyWeights(strategySamples("defensive", RegimeUnknown, 3, 3))
		if got["defensive"] != 1.0 {
			t.Errorf("a 50%% record must be the identity, got %v", got["defensive"])
		}
	})

	t.Run("regime buckets fit alongside the pooled bucket", func(t *testing.T) {
		samples := append(strategySamples("momentum", RegimeTrend, 5, 0),
			strategySamples("momentum", RegimeHighVol, 0, 5)...)
		got := FitStrategyWeights(samples)
		if got["momentum@trend"] != 1.5 || got["momentum@high_vol"] != 0.5 {
			t.Errorf("regime buckets wrong: %v", got)
		}
		// Pooled: 5 wins 5 losses → p̂ = 6/12 = 0.5 → 1.0.
		if got["momentum"] != 1.0 {
			t.Errorf("pooled bucket wrong: %v", got)
		}
	})

	t.Run("unknown regime feeds only the pooled bucket", func(t *testing.T) {
		got := FitStrategyWeights(strategySamples("value", RegimeUnknown, 5, 0))
		if _, ok := got["value@"]; ok {
			t.Error("unknown regime must not create a regime bucket")
		}
	})

	t.Run("anonymous samples are skipped", func(t *testing.T) {
		if got := FitStrategyWeights(strategySamples("", RegimeTrend, 9, 0)); len(got) != 0 {
			t.Errorf("samples without a strategy must be ignored, got %v", got)
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		samples := append(strategySamples("momentum", RegimeTrend, 7, 2),
			strategySamples("value", RegimeRange, 3, 4)...)
		if a, b := FitStrategyWeights(samples), FitStrategyWeights(samples); !reflect.DeepEqual(a, b) {
			t.Errorf("same samples produced different tables: %v vs %v", a, b)
		}
	})
}

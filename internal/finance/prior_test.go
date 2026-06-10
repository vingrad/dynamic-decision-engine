package finance

import (
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

func TestWinProbPrior(t *testing.T) {
	cases := []struct {
		name    string
		f       marketdata.Fundamentals
		returns []float64
		want    float64
		ok      bool
	}{
		{
			name: "no information",
			ok:   false,
		},
		{
			name: "single return is not momentum",
			returns: []float64{
				0.04,
			},
			ok: false,
		},
		{
			name: "cheap PE with positive EPS tilts up",
			f:    marketdata.Fundamentals{PE: 14, PB: 2.0, EPS: 6.6},
			want: 0.54,
			ok:   true,
		},
		{
			name: "very cheap PE and PB",
			f:    marketdata.Fundamentals{PE: 10, PB: 1.2, EPS: 3.0},
			want: 0.58,
			ok:   true,
		},
		{
			name: "rich PE and PB tilt down despite positive EPS",
			f:    marketdata.Fundamentals{PE: 30, PB: 5.0, EPS: 1.3},
			want: 0.47,
			ok:   true,
		},
		{
			name: "loss-maker at extreme valuation",
			f:    marketdata.Fundamentals{PE: 45, PB: 6.0, EPS: -2.0},
			want: 0.39,
			ok:   true,
		},
		{
			// A zero net tilt is no information: reporting it as an informed
			// 0.50 would re-enable the volatility-scaled EV path under an
			// effectively flat prior.
			name: "neutral PE band alone is uninformed",
			f:    marketdata.Fundamentals{PE: 20},
			ok:   false,
		},
		{
			name:    "positive momentum tilts up",
			returns: []float64{0.05, 0.05, 0.05},
			want:    0.53,
			ok:      true,
		},
		{
			name:    "negative momentum tilts down",
			returns: []float64{-0.05, -0.05, -0.05},
			want:    0.47,
			ok:      true,
		},
		{
			name:    "small momentum alone is uninformed",
			returns: []float64{0.01, 0.01},
			ok:      false,
		},
		{
			name:    "maximum bearish tilt stays above the floor",
			f:       marketdata.Fundamentals{PE: 45, PB: 6.0, EPS: -2.0},
			returns: []float64{-0.10, -0.10},
			want:    0.36,
			ok:      true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := WinProbPrior(tc.f, tc.returns)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && !approx(got, tc.want) {
				t.Errorf("prior = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCumulativeReturn(t *testing.T) {
	if _, ok := cumulativeReturn(nil); ok {
		t.Error("nil returns should not be informative")
	}
	got, ok := cumulativeReturn([]float64{0.10, -0.10})
	if !ok || !approx(got, -0.01) {
		t.Errorf("cumulativeReturn = %v ok=%v, want -0.01 true", got, ok)
	}
}

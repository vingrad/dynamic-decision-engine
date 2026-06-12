package finance

import (
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

// dailyBars builds a daily bar series from closes, starting at a fixed date.
func dailyBars(closes []float64) []marketdata.Bar {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]marketdata.Bar, len(closes))
	prev := closes[0]
	for i, c := range closes {
		bars[i] = marketdata.Bar{
			Date: start.AddDate(0, 0, i), Open: prev,
			High: max(prev, c) * 1.001, Low: min(prev, c) * 0.999,
			Close: c, Volume: 100000,
		}
		prev = c
	}
	return bars
}

func TestClassifyRegime(t *testing.T) {
	linearUp := make([]float64, 80)
	for i := range linearUp {
		linearUp[i] = 100 * (1 + 0.002*float64(i)) // steady grind, ER near 1
	}

	sawtooth := make([]float64, 80)
	for i := range sawtooth {
		sawtooth[i] = 100
		if i%2 == 1 {
			sawtooth[i] = 102 // alternating ±2%: total movement huge, net ~0
		}
	}

	crash := make([]float64, 80)
	for i := range crash {
		crash[i] = 100
		if i >= 50 {
			crash[i] = 100 * (1 - 0.01*float64(i-50)) // slow 29% bleed
		}
	}

	wild := make([]float64, 80)
	for i := range wild {
		wild[i] = 100
		if i%2 == 1 {
			wild[i] = 104 // alternating ±4% daily: monthly vol >> 12%
		}
	}

	cases := []struct {
		name string
		bars []marketdata.Bar
		want Regime
	}{
		{"too little history", dailyBars(linearUp[:39]), RegimeUnknown},
		{"steady uptrend", dailyBars(linearUp), RegimeTrend},
		{"sawtooth range", dailyBars(sawtooth), RegimeRange},
		{"slow deep drawdown", dailyBars(crash), RegimeHighVol},
		{"violent chop", dailyBars(wild), RegimeHighVol},
		{"no bars", nil, RegimeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ClassifyRegime(tc.bars)
			if r.Regime != tc.want {
				t.Errorf("regime = %q (note %q), want %q", r.Regime, r.Note, tc.want)
			}
			if r.Note == "" {
				t.Error("every reading must explain itself")
			}
		})
	}

	// Determinism: same bars, same reading.
	a, b := ClassifyRegime(dailyBars(crash)), ClassifyRegime(dailyBars(crash))
	if a != b {
		t.Errorf("classification not deterministic: %+v vs %+v", a, b)
	}
}

func TestCombineRegimes(t *testing.T) {
	r := func(reg Regime) RegimeReading { return RegimeReading{Regime: reg} }
	cases := []struct {
		name     string
		readings []RegimeReading
		want     Regime
	}{
		{"empty", nil, RegimeUnknown},
		{"all unknown", []RegimeReading{r(RegimeUnknown), r(RegimeUnknown)}, RegimeUnknown},
		{"high_vol dominates", []RegimeReading{r(RegimeTrend), r(RegimeTrend), r(RegimeHighVol)}, RegimeHighVol},
		{"trend majority", []RegimeReading{r(RegimeTrend), r(RegimeTrend), r(RegimeRange)}, RegimeTrend},
		{"range majority", []RegimeReading{r(RegimeRange), r(RegimeRange), r(RegimeTrend)}, RegimeRange},
		{"tie is unknown", []RegimeReading{r(RegimeTrend), r(RegimeRange)}, RegimeUnknown},
		{"unknowns do not vote", []RegimeReading{r(RegimeUnknown), r(RegimeTrend)}, RegimeTrend},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CombineRegimes(tc.readings); got != tc.want {
				t.Errorf("CombineRegimes = %q, want %q", got, tc.want)
			}
		})
	}
}

package finance

import (
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

func weeklyBars(n int) []marketdata.Bar {
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	bars := make([]marketdata.Bar, n)
	for i := range bars {
		bars[i] = marketdata.Bar{Date: start.AddDate(0, 0, 7*i), Close: 100}
	}
	return bars
}

func TestBarIntervalDays(t *testing.T) {
	if got := BarIntervalDays(nil); got != 0 {
		t.Errorf("no bars => 0, got %v", got)
	}
	if got := BarIntervalDays(weeklyBars(1)); got != 0 {
		t.Errorf("single bar => 0, got %v", got)
	}
	if got := BarIntervalDays(weeklyBars(6)); !approx(got, 7) {
		t.Errorf("weekly bars => 7, got %v", got)
	}
}

func TestScaledVol(t *testing.T) {
	// 1% weekly vol over a 28-day window: sqrt(28/7) = 2x.
	if got := ScaledVol(0.01, 7, 28); !approx(got, 0.02) {
		t.Errorf("ScaledVol = %v, want 0.02", got)
	}
	// Unknown cadence or window degrades to the per-bar vol.
	if got := ScaledVol(0.01, 0, 28); got != 0.01 {
		t.Errorf("unknown interval should pass through, got %v", got)
	}
	if got := ScaledVol(0.01, 7, 0); got != 0.01 {
		t.Errorf("unknown window should pass through, got %v", got)
	}
}

func TestVolImpliedHorizonDays(t *testing.T) {
	// Daily vol 1% traversing a 10% move: (0.10/0.01)^2 = 100 days.
	if got := VolImpliedHorizonDays(0.10, 0.01, 1); got != 100 {
		t.Errorf("implied horizon = %d, want 100", got)
	}
	// Weekly bars: daily vol = 0.0265/sqrt(7) ≈ 1%, same answer.
	if got := VolImpliedHorizonDays(0.10, 0.026458, 7); got != 100 {
		t.Errorf("weekly-bar implied horizon = %d, want 100", got)
	}
	if got := VolImpliedHorizonDays(0, 0.01, 1); got != 0 {
		t.Errorf("no target => 0, got %d", got)
	}
	if got := VolImpliedHorizonDays(0.10, 0, 1); got != 0 {
		t.Errorf("no vol => 0, got %d", got)
	}
}

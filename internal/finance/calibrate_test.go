package finance

import "testing"

func samplesAt(conf float64, successes, failures int) []CalibrationSample {
	var out []CalibrationSample
	for i := 0; i < successes; i++ {
		out = append(out, CalibrationSample{Confidence: conf, Success: true})
	}
	for i := 0; i < failures; i++ {
		out = append(out, CalibrationSample{Confidence: conf, Success: false})
	}
	return out
}

func TestFitCalibrationEmpty(t *testing.T) {
	curve := FitCalibration(nil)
	if !curve.Empty() {
		t.Fatalf("no samples should fit an empty curve, got %+v", curve)
	}
	if got := curve.Apply(0.7); got != 0.7 {
		t.Errorf("empty curve must be identity, got %v", got)
	}
}

func TestFitCalibrationOverconfidence(t *testing.T) {
	// Stated 0.7 confidence succeeded only half the time; stated 0.3 succeeded
	// a quarter of the time. The curve should pull both toward observation.
	samples := append(samplesAt(0.7, 5, 5), samplesAt(0.3, 1, 3)...)
	curve := FitCalibration(samples)
	if curve.Empty() {
		t.Fatal("expected a fitted curve")
	}
	if got := curve.Apply(0.7); !approx(got, 0.5) {
		t.Errorf("Apply(0.7) = %v, want 0.5 (observed rate)", got)
	}
	if got := curve.Apply(0.3); !approx(got, 0.25) {
		t.Errorf("Apply(0.3) = %v, want 0.25 (observed rate)", got)
	}
	// Between bins the mapping interpolates; outside it clamps to the end bins.
	mid := curve.Apply(0.5)
	if mid <= 0.25 || mid >= 0.5 {
		t.Errorf("Apply(0.5) = %v, want interpolated in (0.25, 0.5)", mid)
	}
	if got := curve.Apply(0.05); !approx(got, 0.25) {
		t.Errorf("Apply below first bin should clamp to it, got %v", got)
	}
	if got := curve.Apply(0.95); !approx(got, 0.5) {
		t.Errorf("Apply above last bin should clamp to it, got %v", got)
	}
}

func TestFitCalibrationPoolsViolators(t *testing.T) {
	// The low-confidence bin observed a HIGHER success rate than the
	// high-confidence bin; PAVA must pool them into one monotone block.
	samples := append(samplesAt(0.3, 3, 1), samplesAt(0.7, 1, 3)...) // 0.75 then 0.25
	curve := FitCalibration(samples)
	if len(curve.Bins) != 1 {
		t.Fatalf("expected violators pooled into one block, got %+v", curve.Bins)
	}
	if !approx(curve.Bins[0].Rate, 0.5) || curve.Bins[0].N != 8 {
		t.Errorf("pooled block = %+v, want rate 0.5 over 8 samples", curve.Bins[0])
	}
}

func TestFitCalibrationMonotone(t *testing.T) {
	samples := append(samplesAt(0.1, 1, 9), samplesAt(0.5, 6, 4)...)
	samples = append(samples, samplesAt(0.9, 9, 1)...)
	curve := FitCalibration(samples)
	for i := 1; i < len(curve.Bins); i++ {
		if curve.Bins[i].Rate < curve.Bins[i-1].Rate {
			t.Fatalf("curve not monotone: %+v", curve.Bins)
		}
	}
	// A well-calibrated region maps approximately onto itself.
	if got := curve.Apply(0.9); !approx(got, 0.9) {
		t.Errorf("Apply(0.9) = %v, want 0.9", got)
	}
}

func TestFitCalibrationDeterministic(t *testing.T) {
	samples := append(samplesAt(0.3, 2, 5), samplesAt(0.7, 6, 2)...)
	a, b := FitCalibration(samples), FitCalibration(samples)
	if len(a.Bins) != len(b.Bins) {
		t.Fatal("nondeterministic fit")
	}
	for i := range a.Bins {
		if a.Bins[i] != b.Bins[i] {
			t.Errorf("bin %d differs: %+v vs %+v", i, a.Bins[i], b.Bins[i])
		}
	}
}

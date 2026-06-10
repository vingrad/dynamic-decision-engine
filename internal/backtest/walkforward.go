package backtest

import (
	"context"
	"fmt"

	"github.com/vingrad/dynamic-decision-engine/internal/finance"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// WalkForwardResult compares raw against calibrated confidence on held-out
// decisions: the curve is fit on the first trainN decisions and evaluated on
// the rest, so the improvement (or lack of it) is out-of-sample evidence.
type WalkForwardResult struct {
	TrainDecisions  int                      `json:"train_decisions"`
	EvalDecisions   int                      `json:"eval_decisions"`
	BrierRaw        float64                  `json:"brier_raw"`
	BrierCalibrated float64                  `json:"brier_calibrated"`
	Curve           finance.CalibrationCurve `json:"curve"`
}

// WalkForward replays the scenarios in order (under the given policy), fits a
// calibration curve on the first trainN decisions and scores the remaining
// decisions with and without the curve. The curve is applied to the recorded
// confidences after the fact — the engine is not re-run — which evaluates the
// mapping itself, exactly what `dde calibrate` would install via policy.
func WalkForward(ctx context.Context, reg *pack.Registry, pol policy.Policy, scenarios []Scenario, trainN int) (WalkForwardResult, error) {
	var decisions []Decision
	cells, err := RunMatrix(ctx, reg, pol, scenarios, map[string]finance.ScoringConfig{"walkforward": effectiveMatrixScoring(pol)})
	if err != nil {
		return WalkForwardResult{}, err
	}
	for _, c := range cells {
		decisions = append(decisions, c.Report.Decisions...)
	}
	if trainN <= 0 || trainN >= len(decisions) {
		return WalkForwardResult{}, fmt.Errorf("backtest: trainN must split %d decisions, got %d", len(decisions), trainN)
	}

	train, eval := decisions[:trainN], decisions[trainN:]
	samples := make([]finance.CalibrationSample, 0, len(train))
	for _, d := range train {
		samples = append(samples, finance.CalibrationSample{Confidence: d.TopConfidence, Success: d.Label == 1})
	}
	curve := finance.FitCalibration(samples)

	res := WalkForwardResult{TrainDecisions: len(train), EvalDecisions: len(eval), Curve: curve}
	for _, d := range eval {
		raw := d.TopConfidence - d.Label
		cal := curve.Apply(d.TopConfidence) - d.Label
		res.BrierRaw += raw * raw
		res.BrierCalibrated += cal * cal
	}
	res.BrierRaw /= float64(len(eval))
	res.BrierCalibrated /= float64(len(eval))
	return res, nil
}

// effectiveMatrixScoring resolves the scoring config the walk-forward replay
// should run under: the policy's investing override when present, else the
// defaults — mirroring what the wire layer would build.
func effectiveMatrixScoring(pol policy.Policy) finance.ScoringConfig {
	if dp, ok := pol.For("investing"); ok && dp.Scoring != nil {
		return *dp.Scoring
	}
	return finance.DefaultScoringConfig()
}

package backtest

import (
	"context"
	"fmt"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/finance"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
	"github.com/vingrad/dynamic-decision-engine/internal/strategy"
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

	// Fitting and evaluation both operate on the PRE-calibration confidence, the
	// domain a freshly installed curve is applied to in production — so the
	// comparison measures exactly what `dde calibrate` would ship, even when the
	// replay policy already carried a curve.
	train, eval := decisions[:trainN], decisions[trainN:]
	samples := make([]finance.CalibrationSample, 0, len(train))
	for _, d := range train {
		samples = append(samples, finance.CalibrationSample{Confidence: rawConfidence(d), Success: d.Label == 1})
	}
	curve := finance.FitCalibration(samples)

	res := WalkForwardResult{TrainDecisions: len(train), EvalDecisions: len(eval), Curve: curve}
	for _, d := range eval {
		raw := rawConfidence(d) - d.Label
		cal := curve.Apply(rawConfidence(d)) - d.Label
		res.BrierRaw += raw * raw
		res.BrierCalibrated += cal * cal
	}
	res.BrierRaw /= float64(len(eval))
	res.BrierCalibrated /= float64(len(eval))
	return res, nil
}

// WalkForwardStrategiesResult compares unweighted against outcome-weighted
// strategy selection on held-out decisions, the strategy analogue of
// WalkForwardResult: the weights are fit on the first trainN decisions and
// the remaining competitions are re-weighed offline.
type WalkForwardStrategiesResult struct {
	TrainDecisions  int                `json:"train_decisions"`
	EvalDecisions   int                `json:"eval_decisions"`
	BrierUnweighted float64            `json:"brier_unweighted"`
	BrierWeighted   float64            `json:"brier_weighted"`
	Weights         map[string]float64 `json:"weights"`
}

// WalkForwardStrategies replays the scenarios with the strategy selector on,
// fits per-strategy weights on the first trainN decisions, and re-weighs the
// remaining decisions' RECORDED competitions with and without the table — the
// engine is not re-run, which evaluates exactly the mapping `dde strategy-fit`
// would install via policy. Both arms score the chosen candidate's recorded
// top confidence against the realized label, so the comparison isolates the
// weight table. Decisions that recorded no competition are skipped.
func WalkForwardStrategies(ctx context.Context, reg *pack.Registry, pol policy.Policy, scenarios []Scenario, trainN int) (WalkForwardStrategiesResult, error) {
	on := true
	cells, err := RunStrategyMatrix(ctx, reg, overrideStrategy(pol, policy.StrategySelection{Enabled: &on}), scenarios)
	if err != nil {
		return WalkForwardStrategiesResult{}, err
	}
	var decisions []Decision
	for _, c := range cells {
		if c.Config != "selector" {
			continue
		}
		for _, d := range c.Report.Decisions {
			if len(d.Candidates) > 0 {
				decisions = append(decisions, d)
			}
		}
	}
	if trainN <= 0 || trainN >= len(decisions) {
		return WalkForwardStrategiesResult{}, fmt.Errorf("backtest: trainN must split %d decisions, got %d", len(decisions), trainN)
	}

	train, eval := decisions[:trainN], decisions[trainN:]
	samples := make([]finance.StrategySample, 0, len(train))
	for _, d := range train {
		samples = append(samples, finance.StrategySample{
			Strategy: d.SelectedStrategy,
			Regime:   finance.Regime(d.Regime),
			Success:  d.Label == 1,
		})
	}
	weights := finance.FitStrategyWeights(samples)

	// Both arms run the SAME offline selection rule (utility argmax over the
	// recorded competition, recorded winner as the all-filtered fallback) and
	// differ only in the weight table — so the comparison isolates the table,
	// not the absence of hysteresis or degraded-mode handling.
	res := WalkForwardStrategiesResult{TrainDecisions: len(train), EvalDecisions: len(eval), Weights: weights}
	pick := func(d Decision, w map[string]float64) int {
		if i := strategy.ReWeigh(d.Candidates, w, d.Regime); i >= 0 {
			return i
		}
		return recordedWinner(d)
	}
	for _, d := range eval {
		unweighted := candidateConfidence(d.Candidates, pick(d, nil)) - d.Label
		reweighed := candidateConfidence(d.Candidates, pick(d, weights)) - d.Label
		res.BrierUnweighted += unweighted * unweighted
		res.BrierWeighted += reweighed * reweighed
	}
	res.BrierUnweighted /= float64(len(eval))
	res.BrierWeighted /= float64(len(eval))
	return res, nil
}

// recordedWinner finds the recorded competition entry of the strategy that
// actually won the decision; -1 when it cannot be resolved.
func recordedWinner(d Decision) int {
	for i, c := range d.Candidates {
		if c.StrategyID == d.SelectedStrategy {
			return i
		}
	}
	return -1
}

// candidateConfidence reads a recorded candidate's top confidence; an
// unresolvable index scores as a coin flip rather than skewing either arm.
func candidateConfidence(cands []domain.StrategyCandidate, i int) float64 {
	if i < 0 || i >= len(cands) {
		return 0.5
	}
	return cands[i].TopConfidence
}

// rawConfidence is a decision's pre-calibration confidence, falling back to the
// shipped confidence for planners (or old records) that don't report one.
func rawConfidence(d Decision) float64 {
	if d.TopRawConfidence > 0 {
		return d.TopRawConfidence
	}
	return d.TopConfidence
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

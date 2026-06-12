package backtest

import (
	"context"
	"strings"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
	"github.com/vingrad/dynamic-decision-engine/internal/wire"
)

// simClock is a mutable clock the harness advances between events so both the
// engine and the finance planner evaluate market data as of the event time.
type simClock struct{ t time.Time }

func (c *simClock) Now() time.Time { return c.t }

// Harness replays a scenario through the engine. It builds its own engine wired to
// the finance planner so the market-data as-of tracks the simulated clock.
type Harness struct {
	eng      *engine.Engine
	sim      *simClock
	provider marketdata.Provider
}

// New builds a harness for the given registry/policy and market-data provider.
func New(reg *pack.Registry, pol policy.Policy, provider marketdata.Provider) *Harness {
	sim := &simClock{}
	router := wire.BuildPlannerRouter(reg, pol, wire.PlannerDeps{
		Base:        llm.NewMockPlanner(),
		DataSources: map[string]wire.DataSource{"marketdata": provider},
		FinanceNow:  sim.Now,
	})
	eng := engine.New(router,
		engine.WithEvaluatorResolver(wire.NewEvaluatorResolver(reg, pol)),
		engine.WithGateResolver(wire.NewGateResolver(reg, pol)),
		engine.WithClock(sim.Now),
	)
	return &Harness{eng: eng, sim: sim, provider: provider}
}

// Run replays the scenario and returns a decision-quality report. Nothing is
// persisted; it uses the engine's stateless Evaluate/Replan primitives directly.
func (h *Harness) Run(ctx context.Context, sc Scenario) (Report, error) {
	start := sc.StartAt
	if start.IsZero() && len(sc.Events) > 0 {
		start = sc.Events[0].At
	}
	h.sim.t = start

	current, err := h.eng.Evaluate(ctx, sc.Goal, "")
	if err != nil {
		return Report{}, err
	}

	rep := Report{Scenario: sc.Name}
	var reacts, correctReacts, shouldKills, noiseEvents, noiseReacts int

	for _, ev := range sc.Events {
		h.sim.t = ev.At
		note := ev.Kind
		if reason, ok := ev.Payload["reason"].(string); ok && reason != "" {
			note += ": " + reason
		}
		res, err := h.eng.Replan(ctx, sc.Goal, current, note, ev.Kind, ev.Payload)
		if err != nil {
			return Report{}, err
		}

		top := topMove(res.Candidate)
		rep.Decisions = append(rep.Decisions, Decision{
			At:               ev.At,
			Kind:             ev.Kind,
			Material:         res.Material,
			Reason:           res.Reason,
			TopMove:          top.Title,
			TopConfidence:    top.Confidence,
			TopRawConfidence: top.RawConfidence,
			ShouldKill:       ev.ShouldKill,
			SelectedStrategy: res.Candidate.Provenance.SelectedStrategy,
			Regime:           res.Candidate.Provenance.Regime,
			Candidates:       res.Candidate.Provenance.StrategyCandidates,
		})
		if s := res.Candidate.Provenance.SelectedStrategy; s != "" {
			if rep.StrategyShare == nil {
				rep.StrategyShare = map[string]int{}
			}
			rep.StrategyShare[s]++
			if n := len(rep.Decisions); n >= 2 {
				if prev := rep.Decisions[n-2].SelectedStrategy; prev != "" && prev != s {
					rep.StrategyFlips++
				}
			}
		}

		if ev.ShouldKill {
			shouldKills++
		} else {
			noiseEvents++
			if res.Material {
				noiseReacts++
			}
		}
		if res.Material {
			reacts++
			rep.VersionsCreated++
			if ev.ShouldKill {
				correctReacts++
			}
			current = res.Candidate
		}
	}

	if reacts > 0 {
		rep.KillPrecision = float64(correctReacts) / float64(reacts)
	}
	if shouldKills > 0 {
		rep.KillRecall = float64(correctReacts) / float64(shouldKills)
	}
	rep.NoiseRobustness = 1
	if noiseEvents > 0 {
		rep.NoiseRobustness = 1 - float64(noiseReacts)/float64(noiseEvents)
	}
	h.scoreCalibration(ctx, &rep, h.sim.t)
	rep.HypotheticalPnL, _ = h.tickerReturn(ctx, tickerFromTitle(topMove(current).Title), start, h.sim.t)
	return rep, nil
}

// scoreCalibration fills per-decision forward returns and the report's Brier
// score. The forward window runs from each decision to the scenario end; it is
// computed after the replay, so no future data ever reaches a decision. The
// outcome label is 1 when the top thesis's forward return is positive; when no
// forward window exists (the scenario's final decision) or no return is
// resolvable, the analyst kill label judges the decision instead — a
// zero-length window must not read as an automatic failure.
func (h *Harness) scoreCalibration(ctx context.Context, rep *Report, end time.Time) {
	if len(rep.Decisions) == 0 {
		return
	}
	var sum float64
	for i := range rep.Decisions {
		d := &rep.Decisions[i]
		fr, ok := h.tickerReturn(ctx, tickerFromTitle(d.TopMove), d.At, end)
		if ok {
			d.ForwardReturn = fr
		}
		switch {
		case !d.At.Before(end): // no forward window: only the analyst label exists
			if !d.ShouldKill {
				d.Label = 1
			}
		case ok && fr > 0:
			d.Label = 1
		case !ok && !d.ShouldKill:
			d.Label = 1
		}
		diff := d.TopConfidence - d.Label
		sum += diff * diff
	}
	rep.BrierScore = sum / float64(len(rep.Decisions))
}

// tickerReturn is the buy-and-hold return of one ticker over [from, to] from
// point-in-time quotes. It reports ok=false when either quote is unavailable.
func (h *Harness) tickerReturn(ctx context.Context, ticker string, from, to time.Time) (float64, bool) {
	if ticker == "" {
		return 0, false
	}
	startQ, err := h.provider.Quote(ctx, ticker, from)
	if err != nil || startQ.Price == 0 {
		return 0, false
	}
	endQ, err := h.provider.Quote(ctx, ticker, to)
	if err != nil {
		return 0, false
	}
	return (endQ.Price - startQ.Price) / startQ.Price, true
}

// tickerFromTitle extracts the ticker from a finance-planner move title
// ("Thesis: ACME" -> "ACME"); non-thesis titles yield "".
func tickerFromTitle(title string) string {
	ticker := strings.TrimPrefix(title, "Thesis: ")
	if ticker == title {
		return ""
	}
	return ticker
}

func topMove(v domain.PlanVersion) domain.RankedMove {
	if len(v.RankedMoves) == 0 {
		return domain.RankedMove{}
	}
	return v.RankedMoves[0]
}

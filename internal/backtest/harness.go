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
	var reacts, correctReacts, shouldKills int

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

		top, conf := topMove(res.Candidate)
		rep.Decisions = append(rep.Decisions, Decision{
			At:            ev.At,
			Kind:          ev.Kind,
			Material:      res.Material,
			Reason:        res.Reason,
			TopMove:       top,
			TopConfidence: conf,
			ShouldKill:    ev.ShouldKill,
		})

		if ev.ShouldKill {
			shouldKills++
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
	rep.HypotheticalPnL = h.hypotheticalPnL(ctx, current, start, h.sim.t)
	return rep, nil
}

// hypotheticalPnL is an illustrative buy-and-hold return of the final top thesis's
// ticker over the scenario window. It is NOT a strategy return.
func (h *Harness) hypotheticalPnL(ctx context.Context, current domain.PlanVersion, from, to time.Time) float64 {
	top, _ := topMove(current)
	ticker := strings.TrimPrefix(top, "Thesis: ")
	if ticker == top || ticker == "" {
		return 0
	}
	startQ, err := h.provider.Quote(ctx, ticker, from)
	if err != nil || startQ.Price == 0 {
		return 0
	}
	endQ, err := h.provider.Quote(ctx, ticker, to)
	if err != nil {
		return 0
	}
	return (endQ.Price - startQ.Price) / startQ.Price
}

func topMove(v domain.PlanVersion) (string, float64) {
	if len(v.RankedMoves) == 0 {
		return "", 0
	}
	m := v.RankedMoves[0]
	return m.Title, m.Confidence
}

package source

import (
	"context"
	"fmt"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// Enricher runs a domain's sources before planning and folds their results into the
// goal Context. It satisfies engine.Enricher (structurally — no engine import). It
// never fails a decision: each source runs under its own timeout with panic
// recovery, and any failure becomes a stale contribution rather than a Go error.
type Enricher struct {
	sources []Source
	timeout time.Duration
	now     func() time.Time
}

// NewEnricher builds an Enricher over the given sources. They are consulted in the
// given order, which fixes the Context append order and therefore the snapshot hash.
// A non-positive timeout means no per-source deadline. A nil clock defaults to
// time.Now.
func NewEnricher(sources []Source, timeout time.Duration, now func() time.Time) *Enricher {
	if now == nil {
		now = time.Now
	}
	return &Enricher{sources: sources, timeout: timeout, now: now}
}

// Enrich fetches from each source, folds the deltas into the goal Context in order,
// and returns one audit record per source. With no sources it is a clean no-op.
func (e *Enricher) Enrich(ctx context.Context, goal domain.Goal, signalKind string, payload map[string]any) (domain.Goal, []domain.SourceContribution) {
	if len(e.sources) == 0 {
		return goal, nil
	}
	q := Query{Goal: goal, SignalKind: signalKind, Payload: payload, AsOf: e.now()}
	contribs := make([]domain.SourceContribution, 0, len(e.sources))
	for _, s := range e.sources {
		res := e.fetchOne(ctx, s, q)
		goal.Context = foldInto(goal.Context, res.Delta)
		contribs = append(contribs, toContribution(res, e.now()))
	}
	return goal, contribs
}

// fetchOne runs a single source under a timeout with panic recovery, translating any
// failure into a stale Result so the decision proceeds.
func (e *Enricher) fetchOne(ctx context.Context, s Source, q Query) (res Result) {
	name := s.Describe().Name
	cctx := ctx
	if e.timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}
	defer func() {
		if r := recover(); r != nil {
			res = Result{SourceName: name, Stale: true, Err: fmt.Sprintf("source panicked: %v", r), FetchedAt: e.now()}
		}
	}()
	out, err := s.Fetch(cctx, q)
	if err != nil {
		return Result{SourceName: name, Stale: true, Err: err.Error(), FetchedAt: e.now()}
	}
	if out.SourceName == "" {
		out.SourceName = name
	}
	if out.FetchedAt.IsZero() {
		out.FetchedAt = e.now()
	}
	return out
}

// foldInto appends a delta's contents onto a copy of the context. Append order is
// preserved (and therefore the snapshot hash is stable) for a given source order.
func foldInto(c domain.Context, d ContextDelta) domain.Context {
	if d.Empty() {
		return c
	}
	if len(d.Facts) > 0 {
		c.Facts = append(append([]string(nil), c.Facts...), d.Facts...)
	}
	if len(d.Assets) > 0 {
		c.Assets = append(append([]domain.Asset(nil), c.Assets...), d.Assets...)
	}
	if len(d.Constraints) > 0 {
		c.Constraints = append(append([]domain.Constraint(nil), c.Constraints...), d.Constraints...)
	}
	return c
}

func toContribution(res Result, fallback time.Time) domain.SourceContribution {
	at := res.FetchedAt
	if at.IsZero() {
		at = fallback
	}
	return domain.SourceContribution{
		SourceName:   res.SourceName,
		FetchedAt:    at,
		Stale:        res.Stale,
		Err:          res.Err,
		Raw:          res.Raw,
		DeltaSummary: summarize(res.Delta),
	}
}

func summarize(d ContextDelta) string {
	if d.Empty() {
		return ""
	}
	return fmt.Sprintf("+%d facts, +%d assets, +%d constraints", len(d.Facts), len(d.Assets), len(d.Constraints))
}

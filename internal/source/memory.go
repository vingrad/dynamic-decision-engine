package source

import "context"

// MemorySource is an in-process read-model exposed as a Source: a deterministic
// lookup over state that some other path (a push webhook handler or a background
// poller) keeps fresh. It is the engine-facing read side of the push/pull split —
// transports write state in, the engine reads it here at plan time. Because it does
// no I/O it is fully deterministic and a natural last-good fallback target.
//
// A lookup miss is not a failure: it returns an empty (non-stale) result, since the
// absence of extra context is a valid state, not an outage.
type MemorySource struct {
	name   string
	domain string
	lookup func(Query) (ContextDelta, bool)
}

// NewMemorySource builds a MemorySource. lookup maps a query to a delta; returning
// ok=false means "no entry" (an empty, non-stale contribution). A nil lookup always
// misses.
func NewMemorySource(name, domain string, lookup func(Query) (ContextDelta, bool)) *MemorySource {
	if name == "" {
		name = "memory"
	}
	return &MemorySource{name: name, domain: domain, lookup: lookup}
}

// Describe implements Source.
func (s *MemorySource) Describe() Descriptor {
	return Descriptor{Name: s.name, Domain: s.domain, Description: "in-process read-model"}
}

// Fetch implements Source. It never returns a Go error.
func (s *MemorySource) Fetch(_ context.Context, q Query) (Result, error) {
	if s.lookup == nil {
		return Result{SourceName: s.name}, nil
	}
	delta, ok := s.lookup(q)
	if !ok {
		return Result{SourceName: s.name}, nil
	}
	return Result{SourceName: s.name, Delta: delta}, nil
}

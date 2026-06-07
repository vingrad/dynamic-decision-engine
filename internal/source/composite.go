package source

import (
	"context"
	"encoding/json"
)

// Composite presents several sources as one Source. Fetch runs each member in
// declared order, concatenates their deltas (order-stable, so the merged result
// hashes the same way every run), and records each member's raw payload under its
// name. A failing member is folded in as stale and never aborts the batch.
//
// It is provided for nesting and for the future agentic-planner mode, where a
// domain's whole source set can be exposed to the model as a single tool. The
// default enrichment path uses Enricher directly for per-source audit granularity.
type Composite struct {
	name        string
	description string
	sources     []Source
}

// NewComposite builds a Composite over the given sources.
func NewComposite(name string, sources []Source) *Composite {
	return &Composite{name: name, description: "composite data source", sources: sources}
}

// Describe implements Source.
func (c *Composite) Describe() Descriptor {
	return Descriptor{Name: c.name, Description: c.description}
}

// Fetch implements Source. It never returns a Go error; member failures surface as
// Stale on the merged result.
func (c *Composite) Fetch(ctx context.Context, q Query) (Result, error) {
	merged := Result{SourceName: c.name}
	raw := map[string]json.RawMessage{}
	for _, s := range c.sources {
		out, err := s.Fetch(ctx, q)
		if err != nil {
			merged.Stale = true
			continue
		}
		merged.Delta.Facts = append(merged.Delta.Facts, out.Delta.Facts...)
		merged.Delta.Assets = append(merged.Delta.Assets, out.Delta.Assets...)
		merged.Delta.Constraints = append(merged.Delta.Constraints, out.Delta.Constraints...)
		if out.Stale {
			merged.Stale = true
		}
		if len(out.Raw) > 0 {
			name := out.SourceName
			if name == "" {
				name = s.Describe().Name
			}
			raw[name] = out.Raw
		}
	}
	if len(raw) > 0 {
		if b, err := json.Marshal(raw); err == nil {
			merged.Raw = b
		}
	}
	return merged, nil
}

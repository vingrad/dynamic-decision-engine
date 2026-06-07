// Package source defines the uniform external-data-source layer the engine consults
// before planning. A Source fetches fresh state (prices, stock, balances, research)
// and returns a normalized delta that is folded into the goal Context, so the
// planner reasons over live facts while the decision stays reproducible: the fetched
// data lands in Context, which is part of the input snapshot hash.
//
// One interface serves every mechanism — HTTP/REST, MCP servers, autonomous AI
// agents, internal read-models. Non-deterministic sources (agents, live APIs) keep
// their non-determinism sealed inside Fetch; the engine sees one Result and records
// it (raw payload, fetch time, identity) in provenance for audit.
//
// The same Describe()/Fetch() pair is the seam for a future agentic-planner mode in
// which the model calls a Source as a tool: Descriptor.InputSchema already matches
// the planner tool schema shape, so adapters need no rewrite.
package source

import (
	"context"
	"encoding/json"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// Descriptor is a source's self-description: enough for per-domain wiring and audit,
// and shaped so a later agentic planner can expose the source as a tool without
// changes. InputSchema uses the same JSON-schema property map the planner tooling
// uses (see llm.planSchema), so Describe() doubles as a tool contract.
type Descriptor struct {
	Name        string         // stable id recorded in provenance, e.g. "marketdata", "crm"
	Domain      string         // decision domain it enriches; "" means any domain
	Description string         // human/LLM-facing summary of what it provides
	InputSchema map[string]any // JSON schema of Fetch params; may be nil
}

// Query is the request handed to a source by the deterministic pre-fetch step. It is
// derived only from the goal and triggering signal so the same decision yields the
// same query. AsOf is the engine clock value at planning time, giving point-in-time
// sources a stable bound.
type Query struct {
	Goal       domain.Goal
	SignalKind string
	Payload    map[string]any
	AsOf       time.Time
}

// ContextDelta is the normalized enrichment a source contributes. Its fields are
// appended to the corresponding domain.Context slices before planning, so they enter
// the input snapshot hash and drive reproducibility. Keep deltas free of timestamps
// or fetch metadata — those belong in the audit record, never in Context.
type ContextDelta struct {
	Facts       []string
	Assets      []domain.Asset
	Constraints []domain.Constraint
}

// Empty reports whether the delta contributes nothing, used to keep no-op sources
// from perturbing the snapshot.
func (d ContextDelta) Empty() bool {
	return len(d.Facts) == 0 && len(d.Assets) == 0 && len(d.Constraints) == 0
}

// Result is one source's contribution: the normalized delta plus a full audit
// record. Stale is true when a fetch failed, timed out, or fell back to a cached
// value; in that case the decision still proceeds with whatever Delta is available
// (often empty). Raw holds the verbatim payload for audit and is never hashed.
type Result struct {
	SourceName string
	Delta      ContextDelta
	Raw        json.RawMessage
	FetchedAt  time.Time
	Stale      bool
	Err        string // non-empty when degraded; recorded, not surfaced as a Go error
}

// Source is the one uniform interface every adapter implements. Describe() supports
// wiring and the future agentic tool-exposure; Fetch() is the deterministic
// pre-fetch entry point.
//
// Operational failures (an API down, a timeout) must be returned as
// Result{Stale: true, Err: ...}, nil so the enrichment step never aborts a decision.
// The error return is reserved for programmer errors (e.g. a nil context).
type Source interface {
	Describe() Descriptor
	Fetch(ctx context.Context, q Query) (Result, error)
}

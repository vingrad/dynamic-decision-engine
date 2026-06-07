// Package source — design notes.
//
// # Two integration modes (one interface)
//
// Phase 1 (built): deterministic pre-fetch. The engine calls Enricher.Enrich before
// planning; each Source.Fetch returns a normalized ContextDelta that is folded into
// the goal Context. Because Context is part of the input snapshot hash
// (llm.inputSnapshotID), fetched data is captured by the snapshot and the decision is
// reproducible from the recorded inputs. Non-deterministic sources (live HTTP, MCP,
// AI agents) keep their non-determinism sealed inside Fetch; only the resulting
// Result is recorded (Raw, FetchedAt, identity) in DecisionProvenance.
//
// Phase 2 (designed, not built): agentic planning. A planner behind a config flag
// lets the model decide what to fetch by calling sources as tools mid-reasoning. No
// adapter changes are required because the seam already exists:
//
//   - Descriptor.InputSchema uses the same JSON-schema property map the planner
//     tooling uses (llm.planSchema), so Describe() doubles as a tool definition.
//   - Fetch(ctx, Query) is the tool's invoke entry point.
//
// The agentic planner would enumerate a domain's sources via the existing
// SourceResolver, turn each Describe() into a provider tool, and run a multi-turn
// loop calling Fetch. The deterministic pre-fetch path and the agentic path are
// mutually exclusive per configuration and consume identical Source instances.
//
// # Determinism contract
//
// Reproducibility means "the same recorded inputs always yield the same decision",
// not "re-fetching the world reproduces the decision". Fetched data must land only in
// ContextDelta (hashed via Context); audit-only fields (Raw, FetchedAt) must never
// enter Context, or they would reintroduce non-determinism into the snapshot.
package source

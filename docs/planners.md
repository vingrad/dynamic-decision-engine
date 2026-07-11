# Planners: providers, BYOK and multi-model

The planner is the reasoning backend that turns a goal + context (+ signal)
into a ranked plan. It sits behind one interface, so swapping it never changes
the engine's semantics — versioning, materiality, provenance and outcomes work
the same for every backend.

## Deterministic by default

The default planner is a deterministic mock that runs without API keys. That
makes the system testable, reproducible, usable offline, suitable for CI, and
private by default — in the default configuration the engine and all decision
data stay on your own hardware; nothing leaves the machine, which makes it a
good fit for a self-hosted home-server deployment.

The investing domain does not use the text planner at all: it plans through a
numeric scoring planner over point-in-time market data (see
[domains.md](domains.md#the-investing-domain)).

## BYOK providers

Real planners ship behind the same interface for **Anthropic Claude**,
**OpenAI**, and **DeepSeek** — select one with
`DDE_PLANNER=anthropic|openai|deepseek` and the matching API key
(`ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `DEEPSEEK_API_KEY`). Each elicits the
structured plan via a forced tool/function call and records token usage and
latency as provenance. (DeepSeek is served through the OpenAI-compatible
adapter.)

Enabling a cloud provider sends your inputs to that provider's API; the
deterministic mock remains the default so the system runs fully local, offline,
and in CI with no key.

Tuning knobs:

| Env | Meaning |
| --- | --- |
| `DDE_LLM_MODEL` | Model id override for the selected provider |
| `DDE_LLM_MAX_TOKENS` | Response token budget |
| `DDE_LLM_BASE_URL` | Custom endpoint for the OpenAI-compatible adapter |

**Local AI inference is planned.** A self-hosted LLM planner (via an
OpenAI-compatible endpoint — specific provider to be decided) is on the
roadmap, so AI-assisted planning will eventually run entirely on your own
hardware with no cloud dependency. (`DDE_LLM_BASE_URL` already accepts a
custom endpoint.)

## Multi-model planning

`DDE_PLANNER=multi` composes models by *role*, each recorded in provenance for
auditability:

* **verify** — one provider proposes, a *different* provider critiques (drops
  weak moves, re-calibrates confidence). Best for calibration + auditability.
* **route** — a cheap model handles the common case; escalates to a stronger
  model on low confidence or when a material signal arrives. A cost lever.
* **ensemble** — several providers run in parallel; agreement on the top move
  scales its confidence (divergence lowers it). An uncertainty signal.

```bash
# verify: Claude proposes, GPT reviews
DDE_PLANNER=multi DDE_MULTI_MODE=verify DDE_MULTI_PROVIDERS=anthropic,openai dde serve

# keyless offline demo (ensemble of two mock planners)
DDE_PLANNER=multi DDE_MULTI_MODE=ensemble DDE_MULTI_PROVIDERS=mock,mock \
  dde evaluate --input examples/founder-growth.json
```

| Env | Meaning |
| --- | --- |
| `DDE_MULTI_MODE` | `verify` \| `route` \| `ensemble` |
| `DDE_MULTI_PROVIDERS` | Ordered provider list (e.g. `anthropic,openai`) |
| `DDE_MULTI_CONFIDENCE_THRESHOLD` | `route`: escalate below this top-move confidence |
| `DDE_MULTI_ESCALATE_ON_SIGNAL` | `route`: always escalate on a material signal |

import type { DecisionProvenance } from "@/lib/api";

// ProvenancePanel surfaces the audit trail behind a plan version: why it was
// generated, from which input snapshot, and with which planner/prompt/model.
export function ProvenancePanel({ provenance }: { provenance: DecisionProvenance }) {
  return (
    <div className="card">
      <strong>Decision provenance</strong>
      <p className="meta" style={{ marginTop: 6 }}>
        {provenance.reasoning_summary}
      </p>
      <dl className="kv" style={{ marginTop: 10 }}>
        <dt>Planner</dt>
        <dd>{provenance.planner}</dd>
        <dt>Prompt version</dt>
        <dd>{provenance.prompt_version}</dd>
        <dt>Model</dt>
        <dd>{provenance.model}</dd>
        <dt>Input snapshot</dt>
        <dd className="mono">{provenance.input_snapshot_id}</dd>
      </dl>
    </div>
  );
}

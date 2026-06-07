import type { RankedMove } from "@/lib/api";
import { Pill } from "./Pill";

// MoveTable renders the ranked action paths of a plan version, including each
// move's confidence, impact/effort/risk, rationale and first experiment.
export function MoveTable({ moves }: { moves: RankedMove[] }) {
  if (!moves || moves.length === 0) {
    return <p className="empty">No ranked moves.</p>;
  }
  return (
    <div className="grid">
      {moves.map((m) => (
        <div className="card" key={m.rank}>
          <div className="row" style={{ justifyContent: "space-between" }}>
            <strong>
              #{m.rank} · {m.title}
            </strong>
            <span className="mono">confidence {(m.confidence * 100).toFixed(0)}%</span>
          </div>
          <div className="confidence-bar">
            <span style={{ width: `${Math.round(m.confidence * 100)}%` }} />
          </div>
          <p>{m.description}</p>
          <div className="row">
            <Pill level={m.expected_impact} label="impact" />
            <Pill level={m.effort} label="effort" />
            <Pill level={m.risk} label="risk" />
          </div>
          <p className="meta" style={{ marginTop: 10 }}>
            <em>Why:</em> {m.rationale}
          </p>

          <details>
            <summary className="meta">First experiment · {m.experiment.title}</summary>
            <div className="kv" style={{ marginTop: 10 }}>
              <dt>Duration</dt>
              <dd>{m.experiment.duration_days} days</dd>
              <dt>Success signals</dt>
              <dd>
                <ul>
                  {m.experiment.success_signals.map((s, i) => (
                    <li key={i}>{s}</li>
                  ))}
                </ul>
              </dd>
              <dt>Kill / pivot</dt>
              <dd>
                <ul>
                  {m.experiment.kill_criteria.map((s, i) => (
                    <li key={i}>{s}</li>
                  ))}
                </ul>
              </dd>
              {m.fallback_moves?.length > 0 && (
                <>
                  <dt>Fallback</dt>
                  <dd>{m.fallback_moves.join("; ")}</dd>
                </>
              )}
            </div>
          </details>
        </div>
      ))}
    </div>
  );
}

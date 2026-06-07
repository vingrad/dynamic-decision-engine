"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { browserApiBase } from "@/lib/api";

// NewGoalForm is a small client component that creates a goal directly against
// the API from the browser, then navigates to the new goal's detail page.
export function NewGoalForm() {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [objective, setObjective] = useState("");
  const [metric, setMetric] = useState("");
  const [target, setTarget] = useState("");
  const [asset, setAsset] = useState("");
  const [constraint, setConstraint] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await fetch(`${browserApiBase()}/v1/goals`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          objective,
          metric,
          target,
          context: {
            assets: asset ? [{ name: asset }] : [],
            constraints: constraint ? [{ name: constraint }] : [],
          },
        }),
      });
      if (!res.ok) throw new Error(`Failed to create goal (${res.status})`);
      const goal = await res.json();
      router.push(`/goals/${goal.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
    } finally {
      setBusy(false);
    }
  }

  if (!open) {
    return (
      <button className="btn" onClick={() => setOpen(true)}>
        + New goal
      </button>
    );
  }

  return (
    <form className="card" onSubmit={submit}>
      <label>Objective *</label>
      <input value={objective} onChange={(e) => setObjective(e.target.value)} required />
      <div className="row">
        <div style={{ flex: 1 }}>
          <label>Metric</label>
          <input value={metric} onChange={(e) => setMetric(e.target.value)} />
        </div>
        <div style={{ flex: 1 }}>
          <label>Target</label>
          <input value={target} onChange={(e) => setTarget(e.target.value)} />
        </div>
      </div>
      <label>Key asset</label>
      <input value={asset} onChange={(e) => setAsset(e.target.value)} />
      <label>Key constraint</label>
      <input value={constraint} onChange={(e) => setConstraint(e.target.value)} />
      {error && <p className="error">{error}</p>}
      <div className="row" style={{ marginTop: 14 }}>
        <button className="btn" type="submit" disabled={busy || !objective}>
          {busy ? "Creating…" : "Create goal"}
        </button>
        <button className="btn secondary" type="button" onClick={() => setOpen(false)}>
          Cancel
        </button>
      </div>
    </form>
  );
}

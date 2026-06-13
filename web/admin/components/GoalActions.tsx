"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { browserApiBase } from "@/lib/api";
import { llmHeaders } from "@/lib/byok";

// GoalActions provides the two write operations a reviewer needs from the goal
// detail page: generating the initial plan, and sending a signal that may trigger
// a dynamic re-plan. Both refresh the server-rendered page on success.
export function GoalActions({ goalId, hasPlan }: { goalId: string; hasPlan: boolean }) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [signalKind, setSignalKind] = useState("");
  const [signalDesc, setSignalDesc] = useState("");

  async function generatePlan() {
    setBusy(true);
    setError(null);
    try {
      const res = await fetch(`${browserApiBase()}/v1/goals/${goalId}/plans`, {
        method: "POST",
        headers: llmHeaders(),
      });
      if (!res.ok) throw new Error(`Failed to generate plan (${res.status})`);
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
    } finally {
      setBusy(false);
    }
  }

  async function sendSignal(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await fetch(`${browserApiBase()}/v1/signals`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...llmHeaders() },
        body: JSON.stringify({ goal_id: goalId, kind: signalKind, description: signalDesc }),
      });
      if (!res.ok) throw new Error(`Failed to send signal (${res.status})`);
      setSignalKind("");
      setSignalDesc("");
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="card">
      <strong>Actions</strong>
      {error && <p className="error">{error}</p>}

      {!hasPlan && (
        <div style={{ marginTop: 10 }}>
          <button className="btn" onClick={generatePlan} disabled={busy}>
            {busy ? "Working…" : "Generate initial plan"}
          </button>
        </div>
      )}

      {hasPlan && (
        <form onSubmit={sendSignal} style={{ marginTop: 10 }}>
          <p className="meta">
            Send a signal to re-evaluate the current plan. A material change creates a new
            immutable version.
          </p>
          <div className="row">
            <div style={{ flex: 1 }}>
              <label>Signal kind</label>
              <input
                value={signalKind}
                onChange={(e) => setSignalKind(e.target.value)}
                placeholder="e.g. competitive_shift"
                required
              />
            </div>
          </div>
          <label>Description</label>
          <textarea value={signalDesc} onChange={(e) => setSignalDesc(e.target.value)} rows={2} />
          <div style={{ marginTop: 10 }}>
            <button className="btn" type="submit" disabled={busy || !signalKind}>
              {busy ? "Sending…" : "Send signal"}
            </button>
          </div>
        </form>
      )}
    </div>
  );
}

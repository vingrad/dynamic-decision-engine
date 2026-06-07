import Link from "next/link";
import { listGoals, type Goal } from "@/lib/api";
import { NewGoalForm } from "@/components/NewGoalForm";

// Always render at request time; this page reflects live API state.
export const dynamic = "force-dynamic";

export default async function DashboardPage() {
  let goals: Goal[] = [];
  let error: string | null = null;
  try {
    goals = await listGoals();
  } catch (err) {
    error = err instanceof Error ? err.message : "Failed to reach the API";
  }

  return (
    <>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <div>
          <h1>Goals</h1>
          <p className="subtitle">
            Decision goals and their current ranked action paths.
          </p>
        </div>
        <NewGoalForm />
      </div>

      {error && (
        <div className="card error">
          Could not load goals: {error}. Is the API running?
        </div>
      )}

      {!error && goals.length === 0 && (
        <p className="empty">No goals yet. Create one to generate a plan.</p>
      )}

      <div className="grid">
        {goals.map((g) => (
          <Link key={g.id} href={`/goals/${g.id}`} className="card" style={{ display: "block" }}>
            <strong>{g.objective}</strong>
            <div className="meta" style={{ marginTop: 4 }}>
              {g.metric ? `Metric: ${g.metric}` : "No metric"}
              {g.target ? ` · Target: ${g.target}` : ""}
            </div>
            <div className="mono" style={{ marginTop: 6 }}>
              {g.id}
            </div>
          </Link>
        ))}
      </div>
    </>
  );
}

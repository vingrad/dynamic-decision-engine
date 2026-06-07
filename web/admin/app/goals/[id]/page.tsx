import Link from "next/link";
import { getGoal, getGoalPlan, type Goal, type PlanWithCurrent } from "@/lib/api";
import { MoveTable } from "@/components/MoveTable";
import { GoalActions } from "@/components/GoalActions";

export const dynamic = "force-dynamic";

export default async function GoalPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  let goal: Goal | null = null;
  let plan: PlanWithCurrent | null = null;
  let error: string | null = null;
  try {
    goal = await getGoal(id);
    plan = await getGoalPlan(id);
  } catch (err) {
    error = err instanceof Error ? err.message : "Failed to reach the API";
  }

  if (error) {
    return <div className="card error">Could not load goal: {error}</div>;
  }
  if (!goal) {
    return <div className="card error">Goal not found.</div>;
  }

  const ctx = goal.context || {};

  return (
    <>
      <p>
        <Link href="/">← Goals</Link>
      </p>
      <h1>{goal.objective}</h1>
      <p className="subtitle">
        {goal.metric ? `Metric: ${goal.metric}` : "No metric"}
        {goal.target ? ` · Target: ${goal.target}` : ""}
      </p>

      <div className="card">
        <strong>Context</strong>
        {ctx.situation && <p style={{ marginTop: 6 }}>{ctx.situation}</p>}
        <dl className="kv" style={{ marginTop: 10 }}>
          <dt>Assets</dt>
          <dd>{ctx.assets?.length ? ctx.assets.map((a) => a.name).join(", ") : "—"}</dd>
          <dt>Constraints</dt>
          <dd>{ctx.constraints?.length ? ctx.constraints.map((c) => c.name).join(", ") : "—"}</dd>
        </dl>
      </div>

      <GoalActions goalId={goal.id} hasPlan={plan !== null} />

      <h2>Current plan</h2>
      {!plan ? (
        <p className="empty">No plan generated yet.</p>
      ) : (
        <>
          <div className="card">
            <div className="row" style={{ justifyContent: "space-between" }}>
              <strong>Version {plan.current_version.version}</strong>
              <Link href={`/plans/${plan.plan.plan_id}`}>Version history →</Link>
            </div>
            <p className="meta" style={{ marginTop: 6 }}>
              {plan.current_version.summary}
            </p>
          </div>
          <MoveTable moves={plan.current_version.ranked_moves} />
        </>
      )}
    </>
  );
}

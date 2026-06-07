import Link from "next/link";
import {
  getPlan,
  listPlanVersions,
  type PlanWithCurrent,
  type PlanVersion,
} from "@/lib/api";
import { MoveTable } from "@/components/MoveTable";
import { ProvenancePanel } from "@/components/ProvenancePanel";
import { VersionList } from "@/components/VersionList";

export const dynamic = "force-dynamic";

export default async function PlanPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  let plan: PlanWithCurrent | null = null;
  let versions: PlanVersion[] = [];
  let error: string | null = null;
  try {
    plan = await getPlan(id);
    versions = await listPlanVersions(id);
  } catch (err) {
    error = err instanceof Error ? err.message : "Failed to reach the API";
  }

  if (error) {
    return <div className="card error">Could not load plan: {error}</div>;
  }
  if (!plan) {
    return <div className="card error">Plan not found.</div>;
  }

  const current = plan.current_version;

  return (
    <>
      <p>
        <Link href={`/goals/${plan.plan.goal_id}`}>← Goal</Link>
      </p>
      <h1>{current.goal}</h1>
      <p className="subtitle">
        Plan <span className="mono">{plan.plan.plan_id}</span> · current version{" "}
        {plan.plan.current_version}
      </p>

      <ProvenancePanel provenance={current.provenance} />

      <h2>Ranked action paths · v{current.version}</h2>
      <MoveTable moves={current.ranked_moves} />

      <h2>Version history</h2>
      <div className="card">
        <VersionList versions={versions} currentVersion={plan.plan.current_version} />
      </div>
    </>
  );
}

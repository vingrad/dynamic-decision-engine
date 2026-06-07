// Typed client for the dynamic-decision-engine REST API. The types mirror the
// backend DTOs (internal/api/dto.go) and domain JSON shapes.

export type Level = "low" | "medium" | "high";

export interface Asset {
  name: string;
  kind?: string;
  description?: string;
}

export interface Constraint {
  name: string;
  kind?: string;
  description?: string;
}

export interface Context {
  situation?: string;
  facts?: string[];
  assets?: Asset[];
  constraints?: Constraint[];
}

export interface Goal {
  id: string;
  player_id?: string;
  objective: string;
  metric?: string;
  target?: string;
  context: Context;
  created_at: string;
}

export interface Experiment {
  title: string;
  duration_days: number;
  success_signals: string[];
  kill_criteria: string[];
}

export interface RankedMove {
  rank: number;
  title: string;
  description: string;
  confidence: number;
  expected_impact: Level;
  effort: Level;
  risk: Level;
  rationale: string;
  experiment: Experiment;
  fallback_moves: string[];
}

export interface DecisionProvenance {
  reasoning_summary: string;
  input_snapshot_id: string;
  planner: string;
  prompt_version: string;
  model: string;
}

export interface PlanVersion {
  plan_id: string;
  version: number;
  goal: string;
  summary: string;
  ranked_moves: RankedMove[];
  provenance: DecisionProvenance;
  input_snapshot_id: string;
  created_at: string;
}

export interface Plan {
  plan_id: string;
  goal_id: string;
  current_version: number;
  created_at: string;
  updated_at: string;
}

export interface PlanWithCurrent {
  plan: Plan;
  current_version: PlanVersion;
}

// apiBase returns the backend base URL for server-side requests. Inside Docker
// this points at the api service; locally it falls back to localhost.
function apiBase(): string {
  return (
    process.env.DDE_API_URL ||
    process.env.NEXT_PUBLIC_DDE_API_URL ||
    "http://localhost:8080"
  );
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${apiBase()}${path}`, { cache: "no-store" });
  if (!res.ok) {
    throw new Error(`API ${path} failed: ${res.status}`);
  }
  return (await res.json()) as T;
}

export async function listGoals(): Promise<Goal[]> {
  const data = await get<{ goals: Goal[] }>("/v1/goals");
  return data.goals ?? [];
}

export async function getGoal(id: string): Promise<Goal> {
  return get<Goal>(`/v1/goals/${id}`);
}

// getGoalPlan returns the plan for a goal, or null if none has been generated.
export async function getGoalPlan(id: string): Promise<PlanWithCurrent | null> {
  const res = await fetch(`${apiBase()}/v1/goals/${id}/plans`, { cache: "no-store" });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`API plan lookup failed: ${res.status}`);
  return (await res.json()) as PlanWithCurrent;
}

export async function getPlan(id: string): Promise<PlanWithCurrent> {
  return get<PlanWithCurrent>(`/v1/plans/${id}`);
}

export async function listPlanVersions(id: string): Promise<PlanVersion[]> {
  const data = await get<{ versions: PlanVersion[] }>(`/v1/plans/${id}/versions`);
  return data.versions ?? [];
}

// browserApiBase is used by client components that call the API directly from the
// browser; it must be a publicly reachable URL.
export function browserApiBase(): string {
  return process.env.NEXT_PUBLIC_DDE_API_URL || "http://localhost:8080";
}

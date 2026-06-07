package llm

import "context"

// PlannerRouter dispatches a planning request to a per-domain planner based on the
// goal's Domain, falling back to a default planner. It is itself a Planner, so the
// engine holds exactly one planner and remains unaware that routing happens —
// making per-domain planner selection a first-class, scalable concern rather than
// a special case.
type PlannerRouter struct {
	routes map[string]Planner
	def    Planner
}

// NewPlannerRouter builds a router from a domain->planner map and a default. The
// default is used for the empty domain and for any domain not present in routes.
func NewPlannerRouter(def Planner, routes map[string]Planner) *PlannerRouter {
	if routes == nil {
		routes = map[string]Planner{}
	}
	return &PlannerRouter{routes: routes, def: def}
}

// Name implements Planner.
func (*PlannerRouter) Name() string { return "router" }

// GeneratePlan routes on req.Goal.Domain.
func (r *PlannerRouter) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	if p, ok := r.routes[req.Goal.Domain]; ok && req.Goal.Domain != "" {
		return p.GeneratePlan(ctx, req)
	}
	return r.def.GeneratePlan(ctx, req)
}

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Handler builds the chi router with the full middleware chain and routes.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	// Middleware chain: request IDs, structured logging, panic recovery,
	// per-request timeout and Prometheus metrics. Order matters — RequestID first
	// so it is available to everything downstream; Recoverer late so it wraps
	// handler panics; metrics outermost-but-after-routing via RoutePattern.
	//
	// chi's RealIP is intentionally omitted: it trusts client-supplied
	// X-Forwarded-For / X-Real-IP headers and is spoofable unless a trusted proxy
	// sets them. Add it back behind a proxy that strips inbound forwarding headers.
	r.Use(middleware.RequestID)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(s.cfg.RequestTimeout))
	r.Use(s.corsMiddleware)
	r.Use(s.metrics.middleware)

	// Operational endpoints.
	r.Get("/health", s.handleLive)
	r.Get("/health/live", s.handleLive)
	r.Get("/health/ready", s.handleReady)
	r.Handle("/metrics", s.metrics.Handler())

	// Versioned API.
	r.Route("/v1", func(r chi.Router) {
		r.Route("/goals", func(r chi.Router) {
			r.Post("/", s.handleCreateGoal)
			r.Get("/", s.handleListGoals)
			r.Get("/{id}", s.handleGetGoal)
			r.Post("/{id}/plans", s.handleCreatePlan)
			r.Get("/{id}/plans", s.handleGetGoalPlan)
		})
		r.Route("/plans", func(r chi.Router) {
			r.Get("/{id}", s.handleGetPlan)
			r.Get("/{id}/versions", s.handleListPlanVersions)
		})
		r.Post("/signals", s.handleCreateSignal)
		r.Get("/signals/{id}", s.handleGetSignal)
		r.Post("/outcomes", s.handleCreateOutcome)
		r.Post("/evaluate", s.handleEvaluate)
	})

	return r
}

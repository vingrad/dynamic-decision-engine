package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the Prometheus collectors for the API and exposes a handler for
// the /metrics endpoint. It uses a dedicated registry so multiple servers (e.g.
// in tests) never collide on the global default registry.
type Metrics struct {
	reg          *prometheus.Registry
	requests     *prometheus.CounterVec
	duration     *prometheus.HistogramVec
	planVersions prometheus.Counter
	replans      *prometheus.CounterVec
}

// NewMetrics constructs and registers the API metrics.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dde_http_requests_total",
			Help: "Total number of HTTP requests by method, route and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dde_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds by method and route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		planVersions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "dde_plan_versions_created_total",
			Help: "Total number of immutable plan versions created.",
		}),
		replans: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dde_replans_total",
			Help: "Total replanning evaluations, labelled by whether the change was material.",
		}, []string{"material"}),
	}
	reg.MustRegister(
		m.requests, m.duration, m.planVersions, m.replans,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
}

// Handler serves the Prometheus exposition format for this server's registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// middleware records request count and latency. The route label uses the chi
// route pattern (e.g. "/v1/goals/{id}") rather than the raw path, keeping
// cardinality bounded.
func (m *Metrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		m.requests.WithLabelValues(r.Method, route, strconv.Itoa(ww.Status())).Inc()
		m.duration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// PlanVersionCreated increments the plan-version counter.
func (m *Metrics) PlanVersionCreated() { m.planVersions.Inc() }

// ReplanEvaluated records a replanning evaluation outcome.
func (m *Metrics) ReplanEvaluated(material bool) {
	m.replans.WithLabelValues(strconv.FormatBool(material)).Inc()
}

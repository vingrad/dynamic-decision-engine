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
	planVersions *prometheus.CounterVec
	replans      *prometheus.CounterVec
	replanQueue  *prometheus.CounterVec
	planCache    *prometheus.CounterVec
	webhooks     *prometheus.CounterVec
}

// domainLabel normalises an empty domain to "generic" so the metric label is
// always meaningful and cardinality stays bounded to the registered packs.
func domainLabel(domain string) string {
	if domain == "" {
		return "generic"
	}
	return domain
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
		planVersions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dde_plan_versions_created_total",
			Help: "Total number of immutable plan versions created, by domain.",
		}, []string{"domain"}),
		replans: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dde_replans_total",
			Help: "Total replanning evaluations, by domain and whether the change was material.",
		}, []string{"domain", "material"}),
		replanQueue: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dde_replan_jobs_total",
			Help: "Replanning jobs by domain and outcome (enqueued|failed).",
		}, []string{"domain", "event"}),
		planCache: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dde_plan_cache_total",
			Help: "Plan-cache lookups by domain and result (hit|miss).",
		}, []string{"domain", "result"}),
		webhooks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dde_webhook_deliveries_total",
			Help: "Webhook deliveries by event type and result (success|failure|dropped).",
		}, []string{"event", "result"}),
	}
	reg.MustRegister(
		m.requests, m.duration, m.planVersions, m.replans, m.replanQueue, m.planCache, m.webhooks,
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

// PlanVersionCreated increments the plan-version counter for a domain.
func (m *Metrics) PlanVersionCreated(domain string) {
	m.planVersions.WithLabelValues(domainLabel(domain)).Inc()
}

// ReplanEvaluated records a replanning evaluation outcome for a domain.
func (m *Metrics) ReplanEvaluated(domain string, material bool) {
	m.replans.WithLabelValues(domainLabel(domain), strconv.FormatBool(material)).Inc()
}

// ReplanEnqueued records that a signal scheduled an async replanning job.
func (m *Metrics) ReplanEnqueued(domain string) {
	m.replanQueue.WithLabelValues(domainLabel(domain), "enqueued").Inc()
}

// ReplanFailed records that an async replanning job errored.
func (m *Metrics) ReplanFailed(domain string) {
	m.replanQueue.WithLabelValues(domainLabel(domain), "failed").Inc()
}

// PlanCacheHit records a plan-cache hit for a domain.
func (m *Metrics) PlanCacheHit(domain string) {
	m.planCache.WithLabelValues(domainLabel(domain), "hit").Inc()
}

// PlanCacheMiss records a plan-cache miss for a domain.
func (m *Metrics) PlanCacheMiss(domain string) {
	m.planCache.WithLabelValues(domainLabel(domain), "miss").Inc()
}

// WebhookDelivery records a webhook delivery result for an event type.
func (m *Metrics) WebhookDelivery(event, result string) {
	m.webhooks.WithLabelValues(event, result).Inc()
}

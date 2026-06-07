package app

// Metrics is the observability hook the service uses to record domain events.
// Defining the interface here (rather than importing the API's concrete metrics)
// keeps the service free of any transport dependency — the API's *Metrics simply
// satisfies this interface.
// All methods take a domain label (the goal's decision domain, empty == generic)
// so cost, latency and materiality can be observed per domain. Cardinality stays
// bounded because the label set is the fixed list of registered packs.
type Metrics interface {
	// PlanVersionCreated records that a new immutable plan version was persisted.
	PlanVersionCreated(domain string)
	// ReplanEvaluated records a replanning evaluation and whether it was material.
	ReplanEvaluated(domain string, material bool)
	// ReplanEnqueued records that a signal scheduled an (async) replanning job.
	ReplanEnqueued(domain string)
	// ReplanFailed records that an async replanning job errored.
	ReplanFailed(domain string)
	// PlanCacheHit/PlanCacheMiss record plan-cache effectiveness.
	PlanCacheHit(domain string)
	PlanCacheMiss(domain string)
}

// nopMetrics is the default no-op Metrics used when none is supplied.
type nopMetrics struct{}

func (nopMetrics) PlanVersionCreated(_ string)      {}
func (nopMetrics) ReplanEvaluated(_ string, _ bool) {}
func (nopMetrics) ReplanEnqueued(_ string)          {}
func (nopMetrics) ReplanFailed(_ string)            {}
func (nopMetrics) PlanCacheHit(_ string)            {}
func (nopMetrics) PlanCacheMiss(_ string)           {}

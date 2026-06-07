package app

// Metrics is the observability hook the service uses to record domain events.
// Defining the interface here (rather than importing the API's concrete metrics)
// keeps the service free of any transport dependency — the API's *Metrics simply
// satisfies this interface.
type Metrics interface {
	// PlanVersionCreated records that a new immutable plan version was persisted.
	PlanVersionCreated()
	// ReplanEvaluated records a replanning evaluation and whether it was material.
	ReplanEvaluated(material bool)
}

// nopMetrics is the default no-op Metrics used when none is supplied.
type nopMetrics struct{}

func (nopMetrics) PlanVersionCreated()    {}
func (nopMetrics) ReplanEvaluated(_ bool) {}

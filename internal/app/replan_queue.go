package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// ReplanJob is a unit of replanning work: re-evaluate a goal's current plan in
// light of a stored signal. It carries the signal note/payload so the handler does
// not need to reload the signal.
type ReplanJob struct {
	GoalID        string
	PlanID        string
	Domain        string
	SignalID      string
	SignalKind    string
	SignalNote    string
	SignalPayload map[string]any
	EnqueuedAt    time.Time
}

// ReplanOutcome is the result of processing a job.
type ReplanOutcome struct {
	Processed bool               // false only when a job was skipped/never run
	Material  bool               // whether a new version was created
	Reason    string             // materiality reason (audit-friendly)
	Version   domain.PlanVersion // the resulting current version
}

// ReplanHandler processes a single job. The service supplies its processReplan.
type ReplanHandler func(ctx context.Context, job ReplanJob) (ReplanOutcome, error)

// Enqueued reports how a job was accepted. Synchronous queues (inline) run the
// handler immediately and return its Outcome; asynchronous queues return
// Synchronous=false and an empty Outcome (the result appears as a new plan version
// once a worker processes it).
type Enqueued struct {
	Synchronous bool
	Outcome     ReplanOutcome
}

// ReplanQueue decouples signal ingestion from the (LLM-bound) replanning work.
// Start is called once by the service with its handler; Enqueue schedules a job;
// Shutdown drains in-flight work.
type ReplanQueue interface {
	Start(h ReplanHandler)
	Enqueue(ctx context.Context, job ReplanJob) (Enqueued, error)
	Shutdown(ctx context.Context) error
}

// ErrQueueClosed is returned by Enqueue after Shutdown.
var ErrQueueClosed = errors.New("app: replan queue closed")

// ---------------------------------------------------------------------------
// InlineQueue: synchronous, in-process. The default — preserves the original
// synchronous ApplySignal semantics for the CLI and tests.
// ---------------------------------------------------------------------------

type InlineQueue struct{ handler ReplanHandler }

// NewInlineQueue returns a queue that runs each job synchronously on Enqueue.
func NewInlineQueue() *InlineQueue { return &InlineQueue{} }

func (q *InlineQueue) Start(h ReplanHandler) { q.handler = h }

func (q *InlineQueue) Enqueue(ctx context.Context, job ReplanJob) (Enqueued, error) {
	if q.handler == nil {
		return Enqueued{}, errors.New("app: inline queue has no handler")
	}
	out, err := q.handler(ctx, job)
	return Enqueued{Synchronous: true, Outcome: out}, err
}

func (q *InlineQueue) Shutdown(context.Context) error { return nil }

// ---------------------------------------------------------------------------
// MemoryQueue: asynchronous, worker-pool backed. Each plan has a FIFO of distinct
// pending jobs so no signal is dropped, processed by a single worker at a time;
// signal-id idempotency dedups retries. CI-safe (no infrastructure); a durable
// Postgres-outbox implementation can replace it behind ReplanQueue for scale.
// ---------------------------------------------------------------------------

type MemoryQueue struct {
	workers int
	log     *slog.Logger
	handler ReplanHandler
	timeout time.Duration // per-job deadline; 0 means no timeout

	maxSeen int

	mu       sync.Mutex
	pending  map[string][]ReplanJob // planID -> FIFO of distinct pending jobs
	queued   map[string]bool        // planID currently awaiting/processing
	seen     map[string]bool        // signalID -> seen (idempotency)
	seenFIFO []string               // insertion order of seen keys, for bounded eviction
	closed   bool

	ch   chan string
	wg   sync.WaitGroup
	once sync.Once
}

// defaultMaxSeen bounds the idempotency set so it cannot grow without limit on a
// long-running process.
const defaultMaxSeen = 100_000

// QueueOption customises a MemoryQueue.
type QueueOption func(*MemoryQueue)

// WithQueueTimeout bounds each replan job with a deadline so a hung planner cannot
// wedge a worker permanently. A non-positive duration leaves jobs unbounded.
func WithQueueTimeout(d time.Duration) QueueOption {
	return func(q *MemoryQueue) { q.timeout = d }
}

// NewMemoryQueue returns an async queue with the given worker count and channel
// buffer. Jobs are kept in a per-plan FIFO so every distinct signal is processed
// (in order); duplicate signal IDs are skipped (idempotency). Work for one plan is
// serialised, so concurrent signals never race on the version write.
func NewMemoryQueue(workers, buffer int, log *slog.Logger, opts ...QueueOption) *MemoryQueue {
	if workers < 1 {
		workers = 1
	}
	if buffer < 1 {
		buffer = 1
	}
	if log == nil {
		log = slog.Default()
	}
	q := &MemoryQueue{
		workers: workers,
		log:     log,
		maxSeen: defaultMaxSeen,
		pending: map[string][]ReplanJob{},
		queued:  map[string]bool{},
		seen:    map[string]bool{},
		ch:      make(chan string, buffer),
	}
	for _, opt := range opts {
		opt(q)
	}
	return q
}

// markSeen records a signal ID for idempotency, evicting the oldest when over the
// cap. Caller must hold q.mu.
func (q *MemoryQueue) markSeen(signalID string) {
	q.seen[signalID] = true
	q.seenFIFO = append(q.seenFIFO, signalID)
	if len(q.seenFIFO) > q.maxSeen {
		oldest := q.seenFIFO[0]
		q.seenFIFO = q.seenFIFO[1:]
		delete(q.seen, oldest)
	}
}

// Start launches the worker pool. Safe to call once.
func (q *MemoryQueue) Start(h ReplanHandler) {
	q.once.Do(func() {
		q.handler = h
		for i := 0; i < q.workers; i++ {
			q.wg.Add(1)
			go q.work()
		}
	})
}

// Enqueue appends a job to its plan's FIFO. Duplicate signal IDs are skipped
// (idempotency); distinct signals are never dropped. The plan id is pushed to the
// channel only when the plan had no pending work, so a worker picks it up once and
// drains the whole FIFO.
func (q *MemoryQueue) Enqueue(_ context.Context, job ReplanJob) (Enqueued, error) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return Enqueued{}, ErrQueueClosed
	}
	if q.seen[job.SignalID] {
		q.mu.Unlock()
		return Enqueued{Synchronous: false}, nil // idempotent no-op
	}
	q.markSeen(job.SignalID)
	q.pending[job.PlanID] = append(q.pending[job.PlanID], job)
	push := !q.queued[job.PlanID]
	if push {
		q.queued[job.PlanID] = true
	}
	q.mu.Unlock()

	if push {
		q.ch <- job.PlanID
	}
	return Enqueued{Synchronous: false}, nil
}

func (q *MemoryQueue) work() {
	defer q.wg.Done()
	for planID := range q.ch {
		// Drain every pending job for this plan, in order. Jobs that arrive mid-drain
		// are appended to the FIFO and picked up here too (queued stays true until the
		// FIFO is empty), so nothing is lost and a plan is processed by one worker.
		for {
			q.mu.Lock()
			fifo := q.pending[planID]
			if len(fifo) == 0 {
				delete(q.pending, planID)
				q.queued[planID] = false
				q.mu.Unlock()
				break
			}
			job := fifo[0]
			q.pending[planID] = fifo[1:]
			q.mu.Unlock()

			if err := q.runJob(job); err != nil {
				q.log.Error("async replan failed", "plan_id", job.PlanID, "signal_id", job.SignalID, "err", err)
			}
		}
	}
}

// runJob invokes the handler under a per-job deadline (when configured), so a hung
// planner is cancelled rather than wedging the worker forever. The deadline bounds
// the whole handler, including its internal conflict/transient retries.
func (q *MemoryQueue) runJob(job ReplanJob) error {
	ctx := context.Background()
	if q.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, q.timeout)
		defer cancel()
	}
	_, err := q.handler(ctx, job)
	return err
}

// Shutdown stops accepting new work and waits for workers to drain or ctx to end.
func (q *MemoryQueue) Shutdown(ctx context.Context) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true
	q.mu.Unlock()
	close(q.ch)

	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

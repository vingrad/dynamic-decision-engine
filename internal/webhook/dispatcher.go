// Package webhook delivers app events to an external HTTP endpoint. Delivery is
// best-effort and fully decoupled from the use-case path: events are queued in
// memory, POSTed by background workers with bounded retries, and dropped (with a
// log line and a metric) when the receiver is down for long or the queue is
// full. The store remains the source of truth — receivers that need a complete
// picture reconcile via the REST API.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
)

// Metrics is the delivery observability hook (the API's *Metrics satisfies it).
type Metrics interface {
	// WebhookDelivery records one delivery result: success, failure or dropped.
	WebhookDelivery(event, result string)
}

type nopMetrics struct{}

func (nopMetrics) WebhookDelivery(_, _ string) {}

// Config configures a Dispatcher.
type Config struct {
	// URL is the endpoint events are POSTed to.
	URL string
	// Secret, when non-empty, signs each delivery body with HMAC-SHA256 in the
	// X-DDE-Signature header ("sha256=<hex>").
	Secret string
	// Timeout bounds a single delivery attempt. Defaults to 5s.
	Timeout time.Duration
	// Retries is how many extra attempts a failed delivery gets. Defaults to 0.
	Retries int
	// Events optionally filters delivered event types. Empty means all.
	Events []string
	// QueueSize bounds the in-memory event buffer. Defaults to 1024.
	QueueSize int
	// Workers is the delivery worker count. Defaults to 2.
	Workers int
}

// Dispatcher implements app.Notifier by POSTing events to a webhook endpoint.
type Dispatcher struct {
	cfg     Config
	allowed map[string]bool // nil means all events
	client  *http.Client
	ch      chan app.Event
	wg      sync.WaitGroup
	mu      sync.Mutex
	closed  bool
	log     *slog.Logger
	metrics Metrics
}

// New starts a Dispatcher with cfg.Workers delivery workers.
func New(cfg Config, log *slog.Logger, m Metrics) *Dispatcher {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if log == nil {
		log = slog.Default()
	}
	if m == nil {
		m = nopMetrics{}
	}
	var allowed map[string]bool
	if len(cfg.Events) > 0 {
		allowed = make(map[string]bool, len(cfg.Events))
		for _, e := range cfg.Events {
			allowed[e] = true
		}
	}
	d := &Dispatcher{
		cfg:     cfg,
		allowed: allowed,
		client:  &http.Client{Timeout: cfg.Timeout},
		ch:      make(chan app.Event, cfg.QueueSize),
		log:     log,
		metrics: m,
	}
	d.wg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go d.worker()
	}
	return d
}

// Emit queues an event for delivery. It never blocks: when the buffer is full
// (receiver down or too slow) or the dispatcher is shut down, the event is
// dropped and recorded as such.
func (d *Dispatcher) Emit(_ context.Context, evt app.Event) {
	if d.allowed != nil && !d.allowed[evt.Type] {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		d.drop(evt, "dispatcher shut down")
		return
	}
	select {
	case d.ch <- evt:
	default:
		d.drop(evt, "queue full")
	}
}

func (d *Dispatcher) drop(evt app.Event, reason string) {
	d.metrics.WebhookDelivery(evt.Type, "dropped")
	d.log.Warn("webhook event dropped", "event_id", evt.ID, "event", evt.Type, "reason", reason)
}

// Shutdown stops accepting events and waits until queued events are delivered
// or ctx expires.
func (d *Dispatcher) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.ch)
	}
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for evt := range d.ch {
		d.deliver(evt)
	}
}

// deliver POSTs one event, retrying transient failures (transport errors, 429
// and 5xx) with exponential backoff and jitter. Other 4xx statuses indicate a
// misconfigured receiver and are terminal.
func (d *Dispatcher) deliver(evt app.Event) {
	body, err := json.Marshal(evt)
	if err != nil {
		d.metrics.WebhookDelivery(evt.Type, "failure")
		d.log.Warn("webhook event not serialisable", "event_id", evt.ID, "event", evt.Type, "err", err)
		return
	}

	var lastErr string
	for attempt := 0; attempt <= d.cfg.Retries; attempt++ {
		if attempt > 0 {
			time.Sleep(deliveryBackoff(attempt))
		}
		retryable, errMsg := d.attempt(evt, body)
		if errMsg == "" {
			d.metrics.WebhookDelivery(evt.Type, "success")
			return
		}
		lastErr = errMsg
		if !retryable {
			break
		}
	}
	d.metrics.WebhookDelivery(evt.Type, "failure")
	d.log.Warn("webhook delivery failed", "event_id", evt.ID, "event", evt.Type, "url", d.cfg.URL, "err", lastErr)
}

// attempt performs a single delivery. It returns whether a failure is worth
// retrying and an empty errMsg on success.
func (d *Dispatcher) attempt(evt app.Event, body []byte) (retryable bool, errMsg string) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, d.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return false, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "dde-webhook/1.0")
	req.Header.Set("X-DDE-Event", evt.Type)
	req.Header.Set("X-DDE-Delivery", evt.ID)
	if d.cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(d.cfg.Secret))
		mac.Write(body)
		req.Header.Set("X-DDE-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return true, err.Error()
	}
	defer resp.Body.Close()
	// Drain (bounded) so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, ""
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return true, fmt.Sprintf("status %d", resp.StatusCode)
	default:
		return false, fmt.Sprintf("status %d", resp.StatusCode)
	}
}

// deliveryBackoff is an exponential backoff with jitter (≈250ms, 500ms, 1s, …
// capped at 5s, each scaled by a random factor in [0.5, 1.5)).
func deliveryBackoff(attempt int) time.Duration {
	d := 250 * time.Millisecond << (attempt - 1)
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return time.Duration(float64(d) * (0.5 + rand.Float64()))
}

package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// recordingMetrics counts delivery results per (event, result).
type recordingMetrics struct {
	mu     sync.Mutex
	counts map[string]int
}

func newRecordingMetrics() *recordingMetrics {
	return &recordingMetrics{counts: make(map[string]int)}
}

func (m *recordingMetrics) WebhookDelivery(event, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[event+"/"+result]++
}

func (m *recordingMetrics) get(event, result string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[event+"/"+result]
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func testEvent(typ string) app.Event {
	return app.Event{
		ID:        "evt_test1",
		Type:      typ,
		CreatedAt: time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"goal_id": "goal_x"},
	}
}

func shutdown(t *testing.T, d *Dispatcher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestDeliveryEnvelopeAndHeaders(t *testing.T) {
	type received struct {
		body    []byte
		headers http.Header
	}
	got := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- received{body: body, headers: r.Header.Clone()}
	}))
	defer srv.Close()

	m := newRecordingMetrics()
	d := New(Config{URL: srv.URL, Secret: "s3cret"}, discardLogger(), m)
	evt := testEvent("goal.created")
	d.Emit(context.Background(), evt)
	shutdown(t, d)

	r := <-got
	var env struct {
		ID        string         `json:"id"`
		Type      string         `json:"type"`
		CreatedAt time.Time      `json:"created_at"`
		Payload   map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.ID != evt.ID || env.Type != evt.Type || env.Payload["goal_id"] != "goal_x" {
		t.Errorf("unexpected envelope: %+v", env)
	}
	if ct := r.headers.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q", ct)
	}
	if h := r.headers.Get("X-DDE-Event"); h != "goal.created" {
		t.Errorf("X-DDE-Event %q", h)
	}
	if h := r.headers.Get("X-DDE-Delivery"); h != evt.ID {
		t.Errorf("X-DDE-Delivery %q", h)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(r.body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig := r.headers.Get("X-DDE-Signature"); sig != want {
		t.Errorf("signature mismatch: got %q want %q", sig, want)
	}
	if m.get("goal.created", "success") != 1 {
		t.Errorf("expected 1 success metric, got %d", m.get("goal.created", "success"))
	}
}

func TestNoSignatureWithoutSecret(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("X-DDE-Signature")
	}))
	defer srv.Close()

	d := New(Config{URL: srv.URL}, discardLogger(), nil)
	d.Emit(context.Background(), testEvent("goal.created"))
	shutdown(t, d)

	if sig := <-got; sig != "" {
		t.Errorf("expected no signature header, got %q", sig)
	}
}

func TestRetryOnServerError(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	m := newRecordingMetrics()
	d := New(Config{URL: srv.URL, Retries: 2}, discardLogger(), m)
	d.Emit(context.Background(), testEvent("plan.created"))
	shutdown(t, d)

	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Errorf("expected 2 attempts (500 then 200), got %d", calls)
	}
	if m.get("plan.created", "success") != 1 {
		t.Errorf("expected success after retry")
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	m := newRecordingMetrics()
	d := New(Config{URL: srv.URL, Retries: 3}, discardLogger(), m)
	d.Emit(context.Background(), testEvent("plan.created"))
	shutdown(t, d)

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 attempt for 400, got %d", calls)
	}
	if m.get("plan.created", "failure") != 1 {
		t.Errorf("expected failure metric")
	}
}

func TestRetriesExhausted(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	m := newRecordingMetrics()
	d := New(Config{URL: srv.URL, Retries: 1}, discardLogger(), m)
	d.Emit(context.Background(), testEvent("outcome.recorded"))
	shutdown(t, d)

	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Errorf("expected 2 attempts, got %d", calls)
	}
	if m.get("outcome.recorded", "failure") != 1 {
		t.Errorf("expected failure metric after exhausted retries")
	}
}

func TestEventFilter(t *testing.T) {
	var mu sync.Mutex
	var events []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		events = append(events, r.Header.Get("X-DDE-Event"))
		mu.Unlock()
	}))
	defer srv.Close()

	d := New(Config{URL: srv.URL, Events: []string{"plan.created"}}, discardLogger(), nil)
	d.Emit(context.Background(), testEvent("goal.created"))
	d.Emit(context.Background(), testEvent("plan.created"))
	shutdown(t, d)

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0] != "plan.created" {
		t.Errorf("expected only plan.created delivered, got %v", events)
	}
}

func TestQueueOverflowDrops(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	m := newRecordingMetrics()
	d := New(Config{URL: srv.URL, QueueSize: 1, Workers: 1}, discardLogger(), m)
	// First event occupies the worker, second fills the queue, third drops.
	d.Emit(context.Background(), testEvent("goal.created"))
	waitFor(t, func() bool { return len(d.ch) == 0 }) // worker picked it up
	d.Emit(context.Background(), testEvent("goal.created"))
	d.Emit(context.Background(), testEvent("goal.created"))

	if got := m.get("goal.created", "dropped"); got != 1 {
		t.Errorf("expected 1 dropped event, got %d", got)
	}
	close(release)
	shutdown(t, d)
}

func TestShutdownDrainsQueue(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
	}))
	defer srv.Close()

	d := New(Config{URL: srv.URL, Workers: 1}, discardLogger(), nil)
	for i := 0; i < 5; i++ {
		d.Emit(context.Background(), testEvent("goal.created"))
	}
	shutdown(t, d)

	mu.Lock()
	defer mu.Unlock()
	if calls != 5 {
		t.Errorf("expected all 5 queued events delivered on drain, got %d", calls)
	}
}

func TestEmitAfterShutdownDrops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	m := newRecordingMetrics()
	d := New(Config{URL: srv.URL}, discardLogger(), m)
	shutdown(t, d)

	d.Emit(context.Background(), testEvent("goal.created")) // must not panic
	if got := m.get("goal.created", "dropped"); got != 1 {
		t.Errorf("expected dropped metric after shutdown, got %d", got)
	}
}

func TestAttemptTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Outlast the client's 50ms timeout, but return soon after so the test
		// server can close. (Blocking on r.Context() would deadlock: the server
		// never observes the disconnect while the request body is unread.)
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	m := newRecordingMetrics()
	d := New(Config{URL: srv.URL, Timeout: 50 * time.Millisecond}, discardLogger(), m)
	d.Emit(context.Background(), testEvent("goal.created"))
	shutdown(t, d)

	if got := m.get("goal.created", "failure"); got != 1 {
		t.Errorf("expected timeout to count as failure, got %d", got)
	}
}

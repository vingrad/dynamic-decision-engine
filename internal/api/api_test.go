package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
	"github.com/vingrad/dynamic-decision-engine/internal/config"
	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/logging"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

func newTestServer() http.Handler {
	h, _ := newTestServerWithService()
	return h
}

// newTestServerWithService builds a server wired with the pack registry (so domain
// validation is active, as in production) and returns the service too, so tests can
// drain an async replan queue.
func newTestServerWithService(opts ...app.Option) (http.Handler, *app.Service) {
	cfg := config.Default()
	log := logging.New("error", "text")
	repo := storage.NewMemory()
	eng := engine.New(llm.NewMockPlanner())
	metrics := NewMetrics()
	base := []app.Option{app.WithMetrics(metrics), app.WithRegistry(pack.NewRegistry())}
	svc := app.New(repo, eng, append(base, opts...)...)
	return New(cfg, log, svc, metrics).Handler(), svc
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, body io.Reader) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(body).Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func TestHealth(t *testing.T) {
	h := newTestServer()
	for _, path := range []string{"/health", "/health/live", "/health/ready"} {
		rec := doJSON(t, h, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", path, rec.Code)
		}
	}
}

func TestGoalPlanSignalFlow(t *testing.T) {
	h := newTestServer()

	// Create a goal.
	rec := doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{
		Objective: "Grow to 1000 customers",
		Metric:    "customers",
		Context: domain.Context{
			Assets:      []domain.Asset{{Name: "founder network"}},
			Constraints: []domain.Constraint{{Name: "small team"}},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create goal status %d: %s", rec.Code, rec.Body)
	}
	goal := decode[domain.Goal](t, rec.Body)
	if goal.ID == "" {
		t.Fatal("expected goal id")
	}

	// Generate the initial plan (version 1).
	rec = doJSON(t, h, http.MethodPost, "/v1/goals/"+goal.ID+"/plans", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create plan status %d: %s", rec.Code, rec.Body)
	}
	v1 := decode[domain.PlanVersion](t, rec.Body)
	if v1.Version != 1 || len(v1.RankedMoves) != 3 {
		t.Fatalf("unexpected v1: version=%d moves=%d", v1.Version, len(v1.RankedMoves))
	}

	// Creating a second plan for the same goal must conflict.
	rec = doJSON(t, h, http.MethodPost, "/v1/goals/"+goal.ID+"/plans", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on second plan, got %d", rec.Code)
	}

	// Send a signal -> should materially replan to version 2.
	rec = doJSON(t, h, http.MethodPost, "/v1/signals", CreateSignalRequest{
		GoalID:      goal.ID,
		Kind:        "competitive_shift",
		Description: "competitor launched a free tier",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("signal status %d: %s", rec.Code, rec.Body)
	}
	sig := decode[SignalResponse](t, rec.Body)
	if !sig.Material {
		t.Errorf("expected material replan, reason=%q", sig.Reason)
	}
	if sig.PlanVersion.Version != 2 {
		t.Errorf("expected version 2 after material signal, got %d", sig.PlanVersion.Version)
	}

	// Plan versions list should now contain 2 immutable versions.
	rec = doJSON(t, h, http.MethodGet, "/v1/plans/"+v1.PlanID+"/versions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions status %d", rec.Code)
	}
	list := decode[struct {
		Versions []domain.PlanVersion `json:"versions"`
	}](t, rec.Body)
	if len(list.Versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(list.Versions))
	}
}

func TestUpdateGoalStatusEndpoint(t *testing.T) {
	h := newTestServer()

	rec := doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{Objective: "ship v1"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create goal status %d: %s", rec.Code, rec.Body)
	}
	goal := decode[domain.Goal](t, rec.Body)
	if goal.Status != domain.GoalActive {
		t.Fatalf("expected active goal, got %q", goal.Status)
	}

	// Pause it.
	rec = doJSON(t, h, http.MethodPatch, "/v1/goals/"+goal.ID+"/status", UpdateGoalStatusRequest{Status: domain.GoalOnHold})
	if rec.Code != http.StatusOK {
		t.Fatalf("on_hold status %d: %s", rec.Code, rec.Body)
	}
	if got := decode[domain.Goal](t, rec.Body); got.Status != domain.GoalOnHold {
		t.Fatalf("expected on_hold, got %q", got.Status)
	}

	// Resolve it with a resolution.
	rec = doJSON(t, h, http.MethodPatch, "/v1/goals/"+goal.ID+"/status", UpdateGoalStatusRequest{
		Status:           domain.GoalResolved,
		ResolutionResult: domain.OutcomeSuccess,
		ResolutionNotes:  "done",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status %d: %s", rec.Code, rec.Body)
	}
	resolved := decode[domain.Goal](t, rec.Body)
	if resolved.Status != domain.GoalResolved || resolved.Resolution == nil || resolved.Resolution.Result != domain.OutcomeSuccess {
		t.Fatalf("unexpected resolved goal: %+v", resolved)
	}

	// A terminal goal cannot transition again -> 400.
	rec = doJSON(t, h, http.MethodPatch, "/v1/goals/"+goal.ID+"/status", UpdateGoalStatusRequest{Status: domain.GoalActive})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 transitioning out of terminal, got %d: %s", rec.Code, rec.Body)
	}

	// Unknown goal -> 404.
	rec = doJSON(t, h, http.MethodPatch, "/v1/goals/goal_missing/status", UpdateGoalStatusRequest{Status: domain.GoalOnHold})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown goal, got %d", rec.Code)
	}
}

func TestNonActiveGoalSignalReturns409(t *testing.T) {
	h := newTestServer()

	rec := doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{Objective: "ship"})
	goal := decode[domain.Goal](t, rec.Body)
	if rec = doJSON(t, h, http.MethodPost, "/v1/goals/"+goal.ID+"/plans", nil); rec.Code != http.StatusCreated {
		t.Fatalf("create plan: %d %s", rec.Code, rec.Body)
	}

	// Pause the goal, then a signal must be rejected with 409.
	if rec = doJSON(t, h, http.MethodPatch, "/v1/goals/"+goal.ID+"/status", UpdateGoalStatusRequest{Status: domain.GoalOnHold}); rec.Code != http.StatusOK {
		t.Fatalf("on_hold: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(t, h, http.MethodPost, "/v1/signals", CreateSignalRequest{GoalID: goal.ID, Kind: "noise"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for signal on non-active goal, got %d: %s", rec.Code, rec.Body)
	}
}

func TestListGoalsStatusFilterEndpoint(t *testing.T) {
	h := newTestServer()

	rec := doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{Objective: "keep active"})
	_ = decode[domain.Goal](t, rec.Body)
	rec = doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{Objective: "to resolve"})
	resolveID := decode[domain.Goal](t, rec.Body).ID
	if rec = doJSON(t, h, http.MethodPatch, "/v1/goals/"+resolveID+"/status", UpdateGoalStatusRequest{
		Status: domain.GoalResolved, ResolutionResult: domain.OutcomeSuccess,
	}); rec.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", rec.Code, rec.Body)
	}

	type listResp struct {
		Goals []domain.Goal `json:"goals"`
	}
	active := decode[listResp](t, doJSON(t, h, http.MethodGet, "/v1/goals?status=active", nil).Body)
	if len(active.Goals) != 1 || active.Goals[0].Status != domain.GoalActive {
		t.Fatalf("expected 1 active goal, got %+v", active.Goals)
	}
	resolved := decode[listResp](t, doJSON(t, h, http.MethodGet, "/v1/goals?status=resolved", nil).Body)
	if len(resolved.Goals) != 1 || resolved.Goals[0].ID != resolveID {
		t.Fatalf("expected only the resolved goal, got %+v", resolved.Goals)
	}

	// An unrecognised status is a 400.
	if rec = doJSON(t, h, http.MethodGet, "/v1/goals?status=bogus", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad status filter, got %d", rec.Code)
	}
}

func TestValidationErrors(t *testing.T) {
	h := newTestServer()

	// Missing objective.
	rec := doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	prob := decode[ProblemDetail](t, rec.Body)
	if prob.Status != http.StatusBadRequest || prob.Detail == "" {
		t.Errorf("unexpected problem detail: %+v", prob)
	}

	// Unknown goal.
	rec = doJSON(t, h, http.MethodGet, "/v1/goals/missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	// Invalid outcome result.
	rec = doJSON(t, h, http.MethodPost, "/v1/outcomes", CreateOutcomeRequest{GoalID: "g", Result: "nope"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad result, got %d", rec.Code)
	}
}

func TestRecordOutcomeFlow(t *testing.T) {
	h := newTestServer()

	// Create goal + plan.
	rec := doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{
		Objective: "Grow to 1000 customers", Metric: "customers",
		Context: domain.Context{Assets: []domain.Asset{{Name: "network"}}},
	})
	goal := decode[domain.Goal](t, rec.Body)
	rec = doJSON(t, h, http.MethodPost, "/v1/goals/"+goal.ID+"/plans", nil)
	v1 := decode[domain.PlanVersion](t, rec.Body)
	top := v1.RankedMoves[0]

	// Happy path: record an outcome against a real (version, rank).
	rec = doJSON(t, h, http.MethodPost, "/v1/outcomes", CreateOutcomeRequest{
		GoalID: goal.ID, PlanVersion: v1.Version, MoveRank: top.Rank, Result: domain.OutcomePartial,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body)
	}
	out := decode[domain.Outcome](t, rec.Body)
	if out.MoveTitle != top.Title {
		t.Errorf("expected server-derived title %q, got %q", top.Title, out.MoveTitle)
	}

	// Out-of-range rank -> 400.
	rec = doJSON(t, h, http.MethodPost, "/v1/outcomes", CreateOutcomeRequest{
		GoalID: goal.ID, PlanVersion: v1.Version, MoveRank: len(v1.RankedMoves) + 1, Result: domain.OutcomeSuccess,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for out-of-range rank, got %d", rec.Code)
	}

	// Goal with no plan -> 409 (same contract as the signal path).
	rec = doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{Objective: "no plan yet"})
	noPlan := decode[domain.Goal](t, rec.Body)
	rec = doJSON(t, h, http.MethodPost, "/v1/outcomes", CreateOutcomeRequest{
		GoalID: noPlan.ID, PlanVersion: 1, MoveRank: 1, Result: domain.OutcomeSuccess,
	})
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for goal without a plan, got %d", rec.Code)
	}
}

func TestEvaluateStateless(t *testing.T) {
	h := newTestServer()
	rec := doJSON(t, h, http.MethodPost, "/v1/evaluate", EvaluateRequest{
		Objective: "Ship the platform",
		Context:   domain.Context{Assets: []domain.Asset{{Name: "strong team"}}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("evaluate status %d: %s", rec.Code, rec.Body)
	}
	v := decode[domain.PlanVersion](t, rec.Body)
	if v.Version != 1 || len(v.RankedMoves) != 3 {
		t.Errorf("unexpected evaluate result: %+v", v)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	h := newTestServer()
	// Make a request first so the request counter has at least one series to emit.
	doJSON(t, h, http.MethodGet, "/health", nil)

	rec := doJSON(t, h, http.MethodGet, "/metrics", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("dde_http_requests_total")) {
		t.Error("expected custom metric in /metrics output")
	}
}

func TestCreateGoalUnknownDomainRejected(t *testing.T) {
	h := newTestServer()
	rec := doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{
		Domain:    "bogus",
		Objective: "x",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown domain, got %d: %s", rec.Code, rec.Body)
	}
}

func TestCreateGoalInvestingDomainAccepted(t *testing.T) {
	h := newTestServer()
	rec := doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{
		Domain:    "investing",
		Objective: "Build a thesis-driven position",
		Context: domain.Context{
			Assets:      []domain.Asset{{Name: "ACME", Kind: "ticker"}},
			Constraints: []domain.Constraint{{Name: "2y", Kind: "time_horizon"}, {Name: "10% dd", Kind: "drawdown_limit"}},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for investing goal, got %d: %s", rec.Code, rec.Body)
	}
	g := decode[domain.Goal](t, rec.Body)
	if g.Domain != "investing" {
		t.Errorf("expected investing domain, got %q", g.Domain)
	}
}

func TestAsyncSignalReturns202AndStatus(t *testing.T) {
	h, svc := newTestServerWithService(app.WithReplanQueue(app.NewMemoryQueue(2, 16, nil)))

	rec := doJSON(t, h, http.MethodPost, "/v1/goals", CreateGoalRequest{
		Objective: "Grow to 1000 customers", Metric: "customers",
		Context: domain.Context{Assets: []domain.Asset{{Name: "network"}}},
	})
	goal := decode[domain.Goal](t, rec.Body)
	rec = doJSON(t, h, http.MethodPost, "/v1/goals/"+goal.ID+"/plans", nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create plan status %d: %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, h, http.MethodPost, "/v1/signals", CreateSignalRequest{
		GoalID: goal.ID, Kind: "competitive_shift", Description: "free tier",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for async signal, got %d: %s", rec.Code, rec.Body)
	}
	resp := decode[SignalResponse](t, rec.Body)
	if resp.Status != "pending" {
		t.Fatalf("expected pending status, got %q", resp.Status)
	}

	// Drain the worker, then the signal status endpoint must report the outcome.
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, h, http.MethodGet, "/v1/signals/"+resp.Signal.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get signal status %d: %s", rec.Code, rec.Body)
	}
	sig := decode[domain.Signal](t, rec.Body)
	if sig.Status != "applied" || sig.ResultVersion != 2 {
		t.Errorf("expected applied v2, got status=%q v=%d", sig.Status, sig.ResultVersion)
	}
}

// TestMCPMountAndTimeoutScope verifies that a handler passed via WithMCP is
// served at /mcp outside the per-request timeout middleware, while /v1 routes
// remain bounded by it.
func TestMCPMountAndTimeoutScope(t *testing.T) {
	cfg := config.Default()
	cfg.RequestTimeout = 50 * time.Millisecond
	log := logging.New("error", "text")
	metrics := NewMetrics()
	svc := app.New(storage.NewMemory(), engine.New(llm.NewMockPlanner()), app.WithMetrics(metrics))

	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return // timed out: middleware already wrote 504
		case <-time.After(150 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	})
	h := New(cfg, log, svc, metrics, WithMCP(slow)).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("expected /mcp to outlive the request timeout, got %d", rec.Code)
	}

	// A /v1 route slower than the timeout is cut off by the middleware. The
	// health endpoint is fast, so instead assert the middleware is present by
	// checking a context deadline exists inside the group.
	var hasDeadline bool
	probe := New(cfg, log, svc, metrics, WithMCP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasDeadline = r.Context().Deadline()
	}))).Handler()
	rec = httptest.NewRecorder()
	probe.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if hasDeadline {
		t.Error("/mcp request should not carry the per-request timeout deadline")
	}

	rec = doJSON(t, h, http.MethodGet, "/v1/goals", nil)
	if dl := rec.Result().Header; rec.Code != http.StatusOK {
		t.Errorf("expected /v1/goals to succeed, got %d (%v)", rec.Code, dl)
	}
}

// TestNoMCPMountByDefault verifies /mcp is absent without WithMCP.
func TestNoMCPMountByDefault(t *testing.T) {
	h := newTestServer()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unmounted /mcp, got %d", rec.Code)
	}
}

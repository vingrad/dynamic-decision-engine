package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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

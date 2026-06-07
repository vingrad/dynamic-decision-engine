package api

import (
	"bytes"
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
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

func newTestServer() http.Handler {
	cfg := config.Default()
	log := logging.New("error", "text")
	repo := storage.NewMemory()
	eng := engine.New(llm.NewMockPlanner())
	metrics := NewMetrics()
	svc := app.New(repo, eng, app.WithMetrics(metrics))
	return New(cfg, log, svc, metrics).Handler()
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

package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// stubSource returns a fixed result (or error/panic) for testing the Enricher.
type stubSource struct {
	name  string
	res   Result
	err   error
	panic bool
	block time.Duration
}

func (s stubSource) Describe() Descriptor { return Descriptor{Name: s.name} }

func (s stubSource) Fetch(ctx context.Context, _ Query) (Result, error) {
	if s.panic {
		panic("boom")
	}
	if s.block > 0 {
		select {
		case <-time.After(s.block):
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	return s.res, s.err
}

func TestEnricherFoldsDeltasInOrder(t *testing.T) {
	e := NewEnricher([]Source{
		stubSource{name: "a", res: Result{Delta: ContextDelta{Facts: []string{"a1"}}}},
		stubSource{name: "b", res: Result{Delta: ContextDelta{Facts: []string{"b1", "b2"}}}},
	}, 0, func() time.Time { return time.Unix(0, 0) })

	goal := domain.Goal{Context: domain.Context{Facts: []string{"orig"}}}
	got, contribs := e.Enrich(context.Background(), goal, "", nil)

	wantFacts := []string{"orig", "a1", "b1", "b2"}
	if len(got.Context.Facts) != len(wantFacts) {
		t.Fatalf("facts = %v, want %v", got.Context.Facts, wantFacts)
	}
	for i, f := range wantFacts {
		if got.Context.Facts[i] != f {
			t.Errorf("facts[%d] = %q, want %q", i, got.Context.Facts[i], f)
		}
	}
	if len(contribs) != 2 {
		t.Fatalf("expected 2 contributions, got %d", len(contribs))
	}
	if contribs[0].DeltaSummary == "" {
		t.Error("expected a delta summary on a non-empty contribution")
	}
}

func TestEnricherDegradesOnError(t *testing.T) {
	e := NewEnricher([]Source{
		stubSource{name: "bad", err: errors.New("api down")},
	}, 0, func() time.Time { return time.Unix(1, 0) })

	goal := domain.Goal{Context: domain.Context{Facts: []string{"orig"}}}
	got, contribs := e.Enrich(context.Background(), goal, "", nil)

	if len(got.Context.Facts) != 1 {
		t.Errorf("a failing source must not change context, got %v", got.Context.Facts)
	}
	if len(contribs) != 1 || !contribs[0].Stale || contribs[0].Err == "" {
		t.Fatalf("expected a stale contribution with an error, got %+v", contribs)
	}
}

func TestEnricherRecoversPanic(t *testing.T) {
	e := NewEnricher([]Source{
		stubSource{name: "panics", panic: true},
		stubSource{name: "ok", res: Result{Delta: ContextDelta{Facts: []string{"x"}}}},
	}, 0, func() time.Time { return time.Unix(2, 0) })

	got, contribs := e.Enrich(context.Background(), domain.Goal{}, "", nil)
	if len(contribs) != 2 {
		t.Fatalf("expected 2 contributions, got %d", len(contribs))
	}
	if !contribs[0].Stale {
		t.Error("panicking source should be recorded stale")
	}
	if len(got.Context.Facts) != 1 || got.Context.Facts[0] != "x" {
		t.Errorf("the surviving source should still fold in, got %v", got.Context.Facts)
	}
}

func TestEnricherTimeoutIsStale(t *testing.T) {
	e := NewEnricher([]Source{
		stubSource{name: "slow", block: time.Second, res: Result{Delta: ContextDelta{Facts: []string{"late"}}}},
	}, 10*time.Millisecond, func() time.Time { return time.Unix(3, 0) })

	got, contribs := e.Enrich(context.Background(), domain.Goal{}, "", nil)
	if len(contribs) != 1 || !contribs[0].Stale {
		t.Fatalf("expected a stale contribution from a timed-out source, got %+v", contribs)
	}
	if len(got.Context.Facts) != 0 {
		t.Errorf("a timed-out source must not change context, got %v", got.Context.Facts)
	}
}

func TestCompositeMergesAndStale(t *testing.T) {
	c := NewComposite("combo", []Source{
		stubSource{name: "a", res: Result{Delta: ContextDelta{Facts: []string{"a1"}}, Raw: []byte(`{"a":1}`)}},
		stubSource{name: "b", err: errors.New("down")},
	})
	res, err := c.Fetch(context.Background(), Query{})
	if err != nil {
		t.Fatalf("composite must not return a Go error: %v", err)
	}
	if !res.Stale {
		t.Error("a failing member should mark the composite stale")
	}
	if len(res.Delta.Facts) != 1 || res.Delta.Facts[0] != "a1" {
		t.Errorf("expected the surviving member's delta, got %v", res.Delta.Facts)
	}
	if len(res.Raw) == 0 {
		t.Error("expected merged raw payload")
	}
}

func TestHTTPSourceHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("goal_id"); got != "g1" {
			t.Errorf("expected goal_id=g1, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"facts":["price is 42"],"constraints":[{"name":"budget"}]}`))
	}))
	defer srv.Close()

	s := NewHTTPSource(HTTPConfig{Name: "feed", Endpoint: srv.URL})
	res, err := s.Fetch(context.Background(), Query{Goal: domain.Goal{ID: "g1"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stale {
		t.Fatalf("expected a fresh result, got stale: %s", res.Err)
	}
	if len(res.Delta.Facts) != 1 || res.Delta.Facts[0] != "price is 42" {
		t.Errorf("unexpected facts: %v", res.Delta.Facts)
	}
	if len(res.Delta.Constraints) != 1 || res.Delta.Constraints[0].Name != "budget" {
		t.Errorf("unexpected constraints: %v", res.Delta.Constraints)
	}
	if len(res.Raw) == 0 {
		t.Error("expected raw payload to be recorded")
	}
}

func TestHTTPSourceNon200IsStale(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewHTTPSource(HTTPConfig{Name: "feed", Endpoint: srv.URL})
	res, err := s.Fetch(context.Background(), Query{})
	if err != nil {
		t.Fatalf("transport failures should not be Go errors: %v", err)
	}
	if !res.Stale || res.Err == "" {
		t.Errorf("expected a stale result on 500, got %+v", res)
	}
}

package source

import (
	"context"
	"errors"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// scriptedSource returns a different Result/error on each call, for exercising the
// caching fallback.
type scriptedSource struct {
	name  string
	calls []func() (Result, error)
	n     int
}

func (s *scriptedSource) Describe() Descriptor { return Descriptor{Name: s.name} }
func (s *scriptedSource) Fetch(context.Context, Query) (Result, error) {
	f := s.calls[s.n%len(s.calls)]
	s.n++
	return f()
}

func TestMemorySourceLookup(t *testing.T) {
	s := NewMemorySource("rm", "purchasing", func(q Query) (ContextDelta, bool) {
		if q.Goal.ID == "g1" {
			return ContextDelta{Facts: []string{"in stock"}}, true
		}
		return ContextDelta{}, false
	})

	hit, _ := s.Fetch(context.Background(), Query{Goal: domain.Goal{ID: "g1"}})
	if hit.Stale || len(hit.Delta.Facts) != 1 {
		t.Errorf("expected a fresh hit, got %+v", hit)
	}
	miss, _ := s.Fetch(context.Background(), Query{Goal: domain.Goal{ID: "other"}})
	if miss.Stale || !miss.Delta.Empty() {
		t.Errorf("a miss should be empty and non-stale, got %+v", miss)
	}
}

func TestCachingSourceServesLastGood(t *testing.T) {
	prim := &scriptedSource{name: "p", calls: []func() (Result, error){
		func() (Result, error) {
			return Result{SourceName: "p", Delta: ContextDelta{Facts: []string{"v1"}}}, nil
		},
		func() (Result, error) { return Result{}, errors.New("down") },
	}}
	c := NewCachingSource(prim)
	q := Query{Goal: domain.Goal{ID: "g1", Domain: "purchasing"}}

	first, err := c.Fetch(context.Background(), q)
	if err != nil || first.Stale || len(first.Delta.Facts) != 1 {
		t.Fatalf("first fetch should be fresh, got %+v err=%v", first, err)
	}

	second, err := c.Fetch(context.Background(), q)
	if err != nil {
		t.Fatalf("caching source should swallow the error: %v", err)
	}
	if !second.Stale || second.Err == "" {
		t.Errorf("expected last-good served as stale, got %+v", second)
	}
	if len(second.Delta.Facts) != 1 || second.Delta.Facts[0] != "v1" {
		t.Errorf("expected the cached delta, got %v", second.Delta.Facts)
	}
}

func TestCachingSourcePassesThroughWhenNothingCached(t *testing.T) {
	prim := &scriptedSource{name: "p", calls: []func() (Result, error){
		func() (Result, error) { return Result{}, errors.New("down") },
	}}
	c := NewCachingSource(prim)
	res, err := c.Fetch(context.Background(), Query{Goal: domain.Goal{ID: "g1"}})
	if err == nil {
		t.Error("with nothing cached the underlying error should pass through")
	}
	_ = res
}

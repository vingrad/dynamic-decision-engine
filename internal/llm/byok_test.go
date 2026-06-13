package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func byokTestRequest() PlanRequest {
	return PlanRequest{Goal: domain.Goal{Objective: "grow revenue"}}
}

func TestByokFallsBackWithoutCredentials(t *testing.T) {
	fb := &recordingPlanner{name: "fallback"}
	p := NewByokPlanner(ByokConfig{Fallback: fb})

	if _, err := p.GeneratePlan(context.Background(), byokTestRequest()); err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if fb.calls != 1 {
		t.Fatalf("expected fallback called once, got %d", fb.calls)
	}
}

func TestByokIncompleteCredentialsUseFallback(t *testing.T) {
	fb := &recordingPlanner{name: "fallback"}
	p := NewByokPlanner(ByokConfig{Fallback: fb})

	// Provider set but no key: treated as absent so we degrade to the fallback.
	ctx := WithCredentials(context.Background(), Credentials{Provider: "anthropic"})
	if _, err := p.GeneratePlan(ctx, byokTestRequest()); err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if fb.calls != 1 {
		t.Fatalf("expected fallback to handle keyless request, got calls=%d", fb.calls)
	}
}

func TestByokBuildsAndCachesPerKey(t *testing.T) {
	p := NewByokPlanner(ByokConfig{})

	first, err := p.plannerFor(Credentials{Provider: "anthropic", Key: "sk-test-1"})
	if err != nil {
		t.Fatalf("plannerFor: %v", err)
	}
	if _, ok := first.(*AnthropicPlanner); !ok {
		t.Fatalf("expected *AnthropicPlanner, got %T", first)
	}

	again, err := p.plannerFor(Credentials{Provider: "anthropic", Key: "sk-test-1"})
	if err != nil {
		t.Fatalf("plannerFor (cached): %v", err)
	}
	if first != again {
		t.Fatalf("expected the same cached planner instance for the same key")
	}

	other, err := p.plannerFor(Credentials{Provider: "openai", Key: "sk-test-2"})
	if err != nil {
		t.Fatalf("plannerFor (openai): %v", err)
	}
	if _, ok := other.(*OpenAIPlanner); !ok {
		t.Fatalf("expected *OpenAIPlanner, got %T", other)
	}
	if other == first {
		t.Fatalf("distinct credentials must not share a planner")
	}
}

func TestByokDefaultsProviderToAnthropic(t *testing.T) {
	p := NewByokPlanner(ByokConfig{})
	pl, err := p.plannerFor(Credentials{Key: "sk-test"})
	if err != nil {
		t.Fatalf("plannerFor: %v", err)
	}
	if _, ok := pl.(*AnthropicPlanner); !ok {
		t.Fatalf("empty provider should default to anthropic, got %T", pl)
	}
}

func TestByokUnsupportedProvider(t *testing.T) {
	p := NewByokPlanner(ByokConfig{})
	_, err := p.plannerFor(Credentials{Provider: "gemini", Key: "sk-test"})
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected ErrUnsupportedProvider, got %v", err)
	}
}

func TestByokCacheKeyDistinguishesProviderAndKey(t *testing.T) {
	a := byokCacheKey("anthropic", "k", "")
	b := byokCacheKey("openai", "k", "")
	c := byokCacheKey("anthropic", "k2", "")
	if a == b || a == c || b == c {
		t.Fatalf("cache keys collided: a=%s b=%s c=%s", a, b, c)
	}
}

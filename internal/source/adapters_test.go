package source

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func TestMCPSourceMapsDelta(t *testing.T) {
	s := NewMCPSource(MCPConfig{
		Name: "crm",
		Tool: "lookup",
		Invoke: func(_ context.Context, tool string, _ map[string]any) (json.RawMessage, error) {
			if tool != "lookup" {
				t.Errorf("unexpected tool %q", tool)
			}
			return json.RawMessage(`{"facts":["vip customer"]}`), nil
		},
	})
	res, err := s.Fetch(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stale || len(res.Delta.Facts) != 1 || res.Delta.Facts[0] != "vip customer" {
		t.Errorf("unexpected result %+v", res)
	}
	if len(res.Raw) == 0 {
		t.Error("expected raw payload recorded")
	}
}

func TestMCPSourceStaleOnError(t *testing.T) {
	s := NewMCPSource(MCPConfig{Name: "crm", Tool: "lookup", Invoke: func(context.Context, string, map[string]any) (json.RawMessage, error) {
		return nil, errors.New("transport down")
	}})
	res, _ := s.Fetch(context.Background(), Query{})
	if !res.Stale || res.Err == "" {
		t.Errorf("expected stale result, got %+v", res)
	}
}

func TestAgentSourceQuarantinesRun(t *testing.T) {
	s := NewAgentSource(AgentConfig{
		Name: "researcher",
		Run: func(_ context.Context, q Query) (ContextDelta, json.RawMessage, error) {
			return ContextDelta{Facts: []string{"market grew 10%"}}, json.RawMessage(`["turn1","turn2"]`), nil
		},
	})
	res, err := s.Fetch(context.Background(), Query{Goal: domain.Goal{ID: "g1"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stale || len(res.Delta.Facts) != 1 {
		t.Errorf("unexpected result %+v", res)
	}
	if len(res.Raw) == 0 {
		t.Error("expected transcript recorded as raw")
	}
}

func TestAgentSourceStaleOnError(t *testing.T) {
	s := NewAgentSource(AgentConfig{Name: "researcher", Run: func(context.Context, Query) (ContextDelta, json.RawMessage, error) {
		return ContextDelta{}, nil, errors.New("model unavailable")
	}})
	res, _ := s.Fetch(context.Background(), Query{})
	if !res.Stale || res.Err == "" {
		t.Errorf("expected stale result, got %+v", res)
	}
}

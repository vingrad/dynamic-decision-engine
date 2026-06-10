package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// connect builds an offline service (memory store + deterministic planner),
// serves it over an in-memory MCP transport and returns a connected client
// session.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	svc := app.New(storage.NewMemory(), engine.New(llm.NewMockPlanner()))
	srv := New(svc, "test")

	clientTr, serverTr := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = srv.Run(ctx, serverTr) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// call invokes a tool and fails the test on a protocol-level error.
func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

// callOK invokes a tool, requires success, and unmarshals the structured
// content into a generic map.
func callOK(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res := call(t, s, name, args)
	if res.IsError {
		t.Fatalf("call %s errored: %s", name, errText(res))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	return out
}

// callErr invokes a tool and requires a tool error containing want.
func callErr(t *testing.T, s *mcp.ClientSession, name string, args map[string]any, want string) {
	t.Helper()
	res := call(t, s, name, args)
	if !res.IsError {
		t.Fatalf("call %s: expected tool error containing %q, got success", name, want)
	}
	if text := errText(res); !strings.Contains(text, want) {
		t.Errorf("call %s: error %q does not contain %q", name, text, want)
	}
}

func errText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestListTools(t *testing.T) {
	s := connect(t)
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"evaluate": false, "create_goal": false, "get_goal": false,
		"list_goals": false, "update_goal_status": false, "generate_plan": false,
		"get_plan": false, "list_plan_versions": false, "submit_signal": false,
		"record_outcome": false,
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; !ok {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		want[tool.Name] = true
		if tool.InputSchema == nil {
			t.Errorf("tool %q has no input schema", tool.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestEvaluateStateless(t *testing.T) {
	s := connect(t)
	out := callOK(t, s, "evaluate", map[string]any{
		"objective": "Grow to 1000 customers",
		"context": map[string]any{
			"assets": []map[string]any{{"name": "founder network"}},
		},
	})
	if out["version"] != float64(1) {
		t.Errorf("expected version 1, got %v", out["version"])
	}
	moves, ok := out["ranked_moves"].([]any)
	if !ok || len(moves) == 0 {
		t.Errorf("expected ranked moves, got %v", out["ranked_moves"])
	}
}

func TestFullDecisionLoop(t *testing.T) {
	s := connect(t)

	goal := callOK(t, s, "create_goal", map[string]any{
		"objective": "Grow to 1000 customers",
		"metric":    "customers",
	})
	goalID, _ := goal["id"].(string)
	if goalID == "" {
		t.Fatalf("no goal id in %v", goal)
	}
	if goal["status"] != "active" {
		t.Errorf("expected active goal, got %v", goal["status"])
	}

	plan := callOK(t, s, "generate_plan", map[string]any{"goal_id": goalID})
	planID, _ := plan["plan_id"].(string)
	if planID == "" || plan["version"] != float64(1) {
		t.Fatalf("unexpected plan: %v", plan)
	}

	sig := callOK(t, s, "submit_signal", map[string]any{
		"goal_id":     goalID,
		"kind":        "competitive_shift",
		"description": "free tier launched",
	})
	if sig["status"] != "applied" || sig["material"] != true {
		t.Errorf("expected applied material signal, got status=%v material=%v", sig["status"], sig["material"])
	}

	versions := callOK(t, s, "list_plan_versions", map[string]any{"plan_id": planID})
	if vs, _ := versions["versions"].([]any); len(vs) != 2 {
		t.Errorf("expected 2 versions after material signal, got %d", len(vs))
	}

	view := callOK(t, s, "get_plan", map[string]any{"goal_id": goalID})
	cur, _ := view["current_version"].(map[string]any)
	if cur["version"] != float64(2) {
		t.Errorf("expected current version 2, got %v", cur["version"])
	}

	outcome := callOK(t, s, "record_outcome", map[string]any{
		"goal_id":      goalID,
		"plan_version": 2,
		"move_rank":    1,
		"result":       "success",
	})
	if outcome["move_title"] == "" {
		t.Errorf("expected server-snapshotted move title, got %v", outcome)
	}

	resolved := callOK(t, s, "update_goal_status", map[string]any{
		"goal_id":           goalID,
		"status":            "resolved",
		"resolution_result": "success",
	})
	if resolved["status"] != "resolved" {
		t.Errorf("expected resolved goal, got %v", resolved["status"])
	}

	goals := callOK(t, s, "list_goals", map[string]any{"status": "resolved"})
	if gs, _ := goals["goals"].([]any); len(gs) != 1 {
		t.Errorf("expected 1 resolved goal, got %v", goals["goals"])
	}
}

func TestToolErrors(t *testing.T) {
	s := connect(t)

	callErr(t, s, "get_goal", map[string]any{"goal_id": "goal_missing"}, "not found")
	callErr(t, s, "evaluate", map[string]any{"objective": ""}, "invalid input")
	callErr(t, s, "list_goals", map[string]any{"status": "bogus"}, "invalid input")
	callErr(t, s, "get_plan", map[string]any{}, "exactly one of plan_id or goal_id")
	callErr(t, s, "get_plan", map[string]any{"plan_id": "p", "goal_id": "g"}, "exactly one of plan_id or goal_id")

	goal := callOK(t, s, "create_goal", map[string]any{"objective": "x"})
	goalID := goal["id"].(string)

	callErr(t, s, "submit_signal", map[string]any{"goal_id": goalID, "kind": "x"}, "no plan exists")

	if _ = callOK(t, s, "generate_plan", map[string]any{"goal_id": goalID}); true {
		callErr(t, s, "generate_plan", map[string]any{"goal_id": goalID}, "conflict")
	}

	callErr(t, s, "record_outcome", map[string]any{
		"goal_id": goalID, "plan_version": 99, "move_rank": 1, "result": "success",
	}, "invalid input")

	callErr(t, s, "update_goal_status", map[string]any{
		"goal_id": goalID, "status": "resolved",
	}, "invalid input")
}

func TestListGoalsPagination(t *testing.T) {
	s := connect(t)
	for i := 0; i < 3; i++ {
		callOK(t, s, "create_goal", map[string]any{"objective": "x"})
	}
	page := callOK(t, s, "list_goals", map[string]any{"limit": 2})
	if gs, _ := page["goals"].([]any); len(gs) != 2 {
		t.Errorf("expected page of 2 goals, got %v", len(gs))
	}
	rest := callOK(t, s, "list_goals", map[string]any{"limit": 2, "offset": 2})
	if gs, _ := rest["goals"].([]any); len(gs) != 1 {
		t.Errorf("expected 1 goal on second page, got %v", len(gs))
	}
}

package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// Tool input types. These mirror the REST DTOs (internal/api/dto.go) but are
// defined here with jsonschema descriptions so the SDK can infer self-documenting
// input schemas for agents.
//
// Output note: tools whose result contains a domain.PlanVersion use an untyped
// (any) output. PlanVersion's provenance carries raw source payloads as
// json.RawMessage, which schema inference types as a byte array — a typed output
// schema would then reject real plans during output validation. Goal and outcome
// results carry no raw JSON and stay fully typed.

type evaluateInput struct {
	Domain     string         `json:"domain,omitempty" jsonschema:"decision domain: generic, investing, growth or career; empty means generic"`
	Objective  string         `json:"objective" jsonschema:"the goal to plan for (required)"`
	Metric     string         `json:"metric,omitempty" jsonschema:"how progress is measured"`
	Target     string         `json:"target,omitempty" jsonschema:"the value or threshold that defines success"`
	Context    domain.Context `json:"context,omitempty" jsonschema:"the known situation: facts, assets and constraints"`
	SignalNote string         `json:"signal_note,omitempty" jsonschema:"optional note about a recent change to fold into the reasoning"`
}

type createGoalInput struct {
	PlayerID  string         `json:"player_id,omitempty" jsonschema:"optional id of the person/team/system pursuing the goal"`
	Domain    string         `json:"domain,omitempty" jsonschema:"decision domain: generic, investing, growth or career; empty means generic"`
	Objective string         `json:"objective" jsonschema:"the goal to plan for (required)"`
	Metric    string         `json:"metric,omitempty" jsonschema:"how progress is measured"`
	Target    string         `json:"target,omitempty" jsonschema:"the value or threshold that defines success"`
	Context   domain.Context `json:"context,omitempty" jsonschema:"the known situation: facts, assets and constraints"`
}

type goalIDInput struct {
	GoalID string `json:"goal_id" jsonschema:"the goal id (required)"`
}

type listGoalsInput struct {
	Status string `json:"status,omitempty" jsonschema:"filter by lifecycle status: active, on_hold, resolved or abandoned; empty means all"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size (default 50, max 200)"`
	Offset int    `json:"offset,omitempty" jsonschema:"page offset"`
}

type updateGoalStatusInput struct {
	GoalID           string `json:"goal_id" jsonschema:"the goal id (required)"`
	Status           string `json:"status" jsonschema:"target lifecycle status: active, on_hold, resolved or abandoned (required)"`
	ResolutionResult string `json:"resolution_result,omitempty" jsonschema:"required when resolving or abandoning: success, failure, partial or inconclusive"`
	ResolutionNotes  string `json:"resolution_notes,omitempty" jsonschema:"optional notes on how the goal concluded"`
}

type getPlanInput struct {
	PlanID string `json:"plan_id,omitempty" jsonschema:"the plan id; provide exactly one of plan_id or goal_id"`
	GoalID string `json:"goal_id,omitempty" jsonschema:"the goal id; provide exactly one of plan_id or goal_id"`
}

type listPlanVersionsInput struct {
	PlanID string `json:"plan_id" jsonschema:"the plan id (required)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"page size (default 50, max 200)"`
	Offset int    `json:"offset,omitempty" jsonschema:"page offset"`
}

type submitSignalInput struct {
	GoalID      string         `json:"goal_id" jsonschema:"the goal the signal applies to (required)"`
	Kind        string         `json:"kind" jsonschema:"signal kind, e.g. competitive_shift, experiment_result, constraint_change (required)"`
	Description string         `json:"description,omitempty" jsonschema:"what happened"`
	Payload     map[string]any `json:"payload,omitempty" jsonschema:"optional structured signal data"`
}

type recordOutcomeInput struct {
	GoalID          string   `json:"goal_id" jsonschema:"the goal the outcome belongs to (required)"`
	PlanVersion     int      `json:"plan_version" jsonschema:"the immutable plan version the executed move came from (required)"`
	MoveRank        int      `json:"move_rank" jsonschema:"the rank of the executed move within that version (required)"`
	Result          string   `json:"result" jsonschema:"the observed result: success, failure, partial or inconclusive (required)"`
	ObservedSignals []string `json:"observed_signals,omitempty" jsonschema:"signals observed while executing the move"`
	Notes           string   `json:"notes,omitempty" jsonschema:"free-form notes"`
}

// listGoalsOutput mirrors the REST list envelope ({"goals": [...]}).
type listGoalsOutput struct {
	Goals []domain.Goal `json:"goals"`
}

// addTools registers the engine's tool set on s, all calling svc directly.
func addTools(s *mcp.Server, svc *app.Service) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "evaluate",
		Description: "Generate a ranked action plan for a self-contained goal without persisting anything. " +
			"Returns ranked moves with confidence, impact/effort/risk, rationale, experiments and fallbacks. " +
			"Use create_goal + generate_plan instead when the decision should be tracked over time.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in evaluateInput) (*mcp.CallToolResult, any, error) {
		version, err := svc.Evaluate(ctx, app.EvaluateInput{
			Domain:     in.Domain,
			Objective:  in.Objective,
			Metric:     in.Metric,
			Target:     in.Target,
			Context:    in.Context,
			SignalNote: in.SignalNote,
		})
		if err != nil {
			return nil, nil, mapErr(err)
		}
		return nil, version, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_goal",
		Description: "Create a persistent goal (objective + context) to plan around. " +
			"Call generate_plan next to produce its initial ranked plan.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createGoalInput) (*mcp.CallToolResult, domain.Goal, error) {
		goal, err := svc.CreateGoal(ctx, app.CreateGoalInput{
			PlayerID:  in.PlayerID,
			Domain:    in.Domain,
			Objective: in.Objective,
			Metric:    in.Metric,
			Target:    in.Target,
			Context:   in.Context,
		})
		return nil, goal, mapErr(err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_goal",
		Description: "Fetch a goal by id, including its lifecycle status and resolution.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in goalIDInput) (*mcp.CallToolResult, domain.Goal, error) {
		goal, err := svc.GetGoal(ctx, in.GoalID)
		return nil, goal, mapErr(err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_goals",
		Description: "List goals, optionally filtered by lifecycle status, paginated.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listGoalsInput) (*mcp.CallToolResult, listGoalsOutput, error) {
		var filter storage.GoalFilter
		if in.Status != "" {
			status := domain.GoalStatus(in.Status)
			if !status.Valid() {
				return nil, listGoalsOutput{}, errors.New("invalid input: status must be one of: active, on_hold, resolved, abandoned")
			}
			filter.Status = status
		}
		goals, err := svc.ListGoals(ctx, filter, storage.Page{Limit: in.Limit, Offset: in.Offset})
		if err != nil {
			return nil, listGoalsOutput{}, mapErr(err)
		}
		return nil, listGoalsOutput{Goals: goals}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_goal_status",
		Description: "Transition a goal's lifecycle status (active, on_hold, resolved, abandoned). " +
			"Resolving or abandoning is terminal and requires a resolution_result.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateGoalStatusInput) (*mcp.CallToolResult, domain.Goal, error) {
		goal, err := svc.UpdateGoalStatus(ctx, app.UpdateGoalStatusInput{
			GoalID:           in.GoalID,
			Status:           domain.GoalStatus(in.Status),
			ResolutionResult: domain.OutcomeResult(in.ResolutionResult),
			ResolutionNotes:  in.ResolutionNotes,
		})
		return nil, goal, mapErr(err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "generate_plan",
		Description: "Generate and persist the initial ranked plan (version 1) for a goal. " +
			"A goal has at most one plan; later changes arrive as new immutable versions via submit_signal.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in goalIDInput) (*mcp.CallToolResult, any, error) {
		version, err := svc.GeneratePlan(ctx, in.GoalID)
		if err != nil {
			return nil, nil, mapErr(err)
		}
		return nil, version, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_plan",
		Description: "Fetch a plan head and its current version, by plan id or by goal id (exactly one).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getPlanInput) (*mcp.CallToolResult, any, error) {
		if (in.PlanID == "") == (in.GoalID == "") {
			return nil, nil, errors.New("invalid input: provide exactly one of plan_id or goal_id")
		}
		var (
			view app.PlanView
			err  error
		)
		if in.PlanID != "" {
			view, err = svc.GetPlan(ctx, in.PlanID)
		} else {
			view, err = svc.GetGoalPlan(ctx, in.GoalID)
		}
		if err != nil {
			return nil, nil, mapErr(err)
		}
		// Same envelope as GET /v1/plans/{id}.
		return nil, map[string]any{"plan": view.Plan, "current_version": view.CurrentVersion}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_plan_versions",
		Description: "List a plan's immutable version history (the decision audit trail), paginated.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listPlanVersionsInput) (*mcp.CallToolResult, any, error) {
		versions, err := svc.ListPlanVersions(ctx, in.PlanID, storage.Page{Limit: in.Limit, Offset: in.Offset})
		if err != nil {
			return nil, nil, mapErr(err)
		}
		// Same envelope as GET /v1/plans/{id}/versions.
		return nil, map[string]any{"versions": versions}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "submit_signal",
		Description: "Report that something changed (a result, a market shift, a constraint change). " +
			"The engine re-evaluates the goal's plan and, if the change is material, appends a new immutable version. " +
			"Status pending means replanning runs asynchronously — poll get_plan or list_plan_versions for the result.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in submitSignalInput) (*mcp.CallToolResult, any, error) {
		result, err := svc.ApplySignal(ctx, app.SignalInput{
			GoalID:      in.GoalID,
			Kind:        in.Kind,
			Description: in.Description,
			Payload:     in.Payload,
		})
		if err != nil {
			return nil, nil, mapErr(err)
		}
		// Same envelope as POST /v1/signals (api.SignalResponse).
		return nil, map[string]any{
			"signal":       result.Signal,
			"status":       string(result.Status),
			"material":     result.Material,
			"reason":       result.Reason,
			"plan_version": result.PlanVersion,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "record_outcome",
		Description: "Record the real-world result of an executed move, addressed by (plan_version, move_rank) " +
			"in the goal's immutable plan. Outcomes build the audit trail; they do not trigger replanning — " +
			"submit a signal if the outcome should change the plan.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in recordOutcomeInput) (*mcp.CallToolResult, domain.Outcome, error) {
		outcome, err := svc.RecordOutcome(ctx, app.OutcomeInput{
			GoalID:          in.GoalID,
			PlanVersion:     in.PlanVersion,
			MoveRank:        in.MoveRank,
			Result:          domain.OutcomeResult(in.Result),
			ObservedSignals: in.ObservedSignals,
			Notes:           in.Notes,
		})
		return nil, outcome, mapErr(err)
	})
}

package llm

import (
	"context"
	"errors"
)

// ErrNotImplemented is returned by placeholder planners that have no real backend
// wired in yet.
var ErrNotImplemented = errors.New("llm: planner not implemented")

// OpenAIConfig holds the settings a real OpenAI-backed planner would need. It is
// defined now so the wiring and configuration surface is stable before the
// implementation lands.
type OpenAIConfig struct {
	APIKey        string
	Model         string
	PromptVersion string
	BaseURL       string // optional override for OpenAI-compatible gateways
}

// OpenAIPlanner is a placeholder for a real model-backed planner. It implements
// the Planner interface so it can be selected and wired exactly like the mock,
// but it does not call any API yet.
//
// To make this real:
//  1. Build a chat/completions request from PlanRequest using a versioned prompt
//     template that asks for the documented ranked-move JSON shape.
//  2. Call the model, parse the structured response into domain.RankedMove values.
//  3. Populate DecisionProvenance and ModelInvocation (model, prompt version,
//     token usage, latency) from the API response.
type OpenAIPlanner struct {
	cfg OpenAIConfig
}

// NewOpenAIPlanner constructs the placeholder planner.
func NewOpenAIPlanner(cfg OpenAIConfig) *OpenAIPlanner {
	return &OpenAIPlanner{cfg: cfg}
}

// Name implements Planner.
func (*OpenAIPlanner) Name() string { return "openai" }

// GeneratePlan implements Planner. It currently returns ErrNotImplemented.
func (*OpenAIPlanner) GeneratePlan(_ context.Context, _ PlanRequest) (PlanResult, error) {
	return PlanResult{}, ErrNotImplemented
}

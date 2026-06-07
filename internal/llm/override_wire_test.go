package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func openaiClientWith(mw openaiopt.Middleware) openai.Client {
	return openai.NewClient(openaiopt.WithAPIKey("test"), openaiopt.WithMaxRetries(0), openaiopt.WithMiddleware(mw))
}

func anthropicClientWith(mw anthropicopt.Middleware) anthropic.Client {
	return anthropic.NewClient(anthropicopt.WithAPIKey("test"), anthropicopt.WithMaxRetries(0), anthropicopt.WithMiddleware(mw))
}

// errShortCircuit stops the SDK after the request is captured; these tests only
// care that the domain prompt override reaches the wire, not that planning
// completes.
var errShortCircuit = fmt.Errorf("captured")

func overrideRequest() PlanRequest {
	return PlanRequest{
		Goal:                 domain.Goal{Domain: "investing", Objective: "Build a position"},
		SystemPromptOverride: "DOMAIN: INVESTING — Not financial advice.",
	}
}

// TestOpenAIPlannerSendsOverride is the regression for the bug where the OpenAI
// adapter ignored req.SystemPromptOverride, silently dropping domain-pack guidance
// for the openai/deepseek providers.
func TestOpenAIPlannerSendsOverride(t *testing.T) {
	var captured string
	mw := func(req *http.Request, _ openaiopt.MiddlewareNext) (*http.Response, error) {
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			captured = string(b)
		}
		return nil, errShortCircuit
	}
	p := NewOpenAIPlanner(OpenAIConfig{Provider: "openai", APIKey: "test"})
	p.client = openaiClientWith(mw)

	_, _ = p.GeneratePlan(context.Background(), overrideRequest())

	if !strings.Contains(captured, "DOMAIN: INVESTING") {
		t.Errorf("OpenAI request did not carry the domain prompt override; system prompt was not composed.\nbody=%s", captured)
	}
}

// TestAnthropicPlannerSendsOverride guards the same behaviour for Anthropic (which
// is already correct) so neither provider can regress.
func TestAnthropicPlannerSendsOverride(t *testing.T) {
	var captured string
	mw := func(req *http.Request, _ anthropicopt.MiddlewareNext) (*http.Response, error) {
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			captured = string(b)
		}
		return nil, errShortCircuit
	}
	p := NewAnthropicPlanner(AnthropicConfig{APIKey: "test"})
	p.client = anthropicClientWith(mw)

	_, _ = p.GeneratePlan(context.Background(), overrideRequest())

	if !strings.Contains(captured, "DOMAIN: INVESTING") {
		t.Errorf("Anthropic request did not carry the domain prompt override.\nbody=%s", captured)
	}
}

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// maxBodyBytes caps request body size to protect the server from oversized payloads.
const maxBodyBytes = 1 << 20 // 1 MiB

// CreateGoalRequest is the body for POST /v1/goals.
type CreateGoalRequest struct {
	PlayerID  string         `json:"player_id"`
	Domain    string         `json:"domain"`
	Objective string         `json:"objective"`
	Metric    string         `json:"metric"`
	Target    string         `json:"target"`
	Context   domain.Context `json:"context"`
}

// CreateSignalRequest is the body for POST /v1/signals.
type CreateSignalRequest struct {
	GoalID      string         `json:"goal_id"`
	Kind        string         `json:"kind"`
	Description string         `json:"description"`
	Payload     map[string]any `json:"payload"`
}

// CreateOutcomeRequest is the body for POST /v1/outcomes.
type CreateOutcomeRequest struct {
	GoalID          string               `json:"goal_id"`
	MoveTitle       string               `json:"move_title"`
	Result          domain.OutcomeResult `json:"result"`
	ObservedSignals []string             `json:"observed_signals"`
	Notes           string               `json:"notes"`
}

// EvaluateRequest is the body for POST /v1/evaluate: a self-contained goal plus
// an optional signal note. It is stateless — nothing is persisted.
type EvaluateRequest struct {
	Domain     string         `json:"domain"`
	Objective  string         `json:"objective"`
	Metric     string         `json:"metric"`
	Target     string         `json:"target"`
	Context    domain.Context `json:"context"`
	SignalNote string         `json:"signal_note"`
}

// SignalResponse is returned by POST /v1/signals: the stored signal plus the
// replanning decision and resulting (possibly new) plan version. When replanning
// runs asynchronously, status is "pending", material is false, and plan_version is
// the current version at acceptance time — poll the plan's versions for the result.
type SignalResponse struct {
	Signal      domain.Signal      `json:"signal"`
	Status      string             `json:"status"`
	Material    bool               `json:"material"`
	Reason      string             `json:"reason"`
	PlanVersion domain.PlanVersion `json:"plan_version"`
}

// decodeJSON reads and strictly decodes a JSON request body into dst.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body is empty")
		}
		return err
	}
	return nil
}

// parsePage extracts limit/offset query parameters into a storage.Page.
func parsePage(r *http.Request) storage.Page {
	q := r.URL.Query()
	page := storage.Page{}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil {
		page.Limit = v
	}
	if v, err := strconv.Atoi(q.Get("offset")); err == nil {
		page.Offset = v
	}
	return page.Normalize()
}

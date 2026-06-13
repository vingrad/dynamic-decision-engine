package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// ProblemDetail is an RFC 7807-style error envelope returned for all error
// responses, giving clients a consistent, machine-readable error shape.
type ProblemDetail struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// writeJSON encodes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// writeError writes a ProblemDetail with the given status, title and detail.
func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, ProblemDetail{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
}

// writeServiceError maps application- and storage-layer errors to HTTP responses.
// Validation errors surface their message; conflict/not-found map to 409/404;
// anything else becomes a 500 without leaking internal detail.
func writeServiceError(w http.ResponseWriter, err error) {
	var validation *app.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Msg)
	case errors.Is(err, llm.ErrUnsupportedProvider):
		writeError(w, http.StatusBadRequest, "unsupported LLM provider; use anthropic, openai, or deepseek")
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource not found")
	case errors.Is(err, app.ErrPlanExists):
		writeError(w, http.StatusConflict, "a plan already exists for this goal")
	case errors.Is(err, app.ErrNoPlanForGoal):
		writeError(w, http.StatusConflict, "no plan exists for this goal; create one first")
	case errors.Is(err, app.ErrGoalNotActive):
		writeError(w, http.StatusConflict, "goal is not active; resume it before sending signals or generating plans")
	case errors.Is(err, storage.ErrConflict):
		writeError(w, http.StatusConflict, "resource conflict")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

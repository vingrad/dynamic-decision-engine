package mcpserver

import (
	"errors"
	"fmt"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// mapErr shapes service-layer errors into tool-error messages, mirroring the
// REST API's status mapping (internal/api/errors.go). The SDK turns a returned
// error into a CallToolResult with IsError set, so mapping here is about
// message clarity and not leaking internals.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var validation *app.ValidationError
	switch {
	case errors.As(err, &validation):
		return fmt.Errorf("invalid input: %s", validation.Msg)
	case errors.Is(err, storage.ErrNotFound):
		return errors.New("not found")
	case errors.Is(err, app.ErrPlanExists):
		return errors.New("conflict: a plan already exists for this goal")
	case errors.Is(err, app.ErrNoPlanForGoal):
		return errors.New("conflict: no plan exists for this goal; call generate_plan first")
	case errors.Is(err, app.ErrGoalNotActive):
		return errors.New("conflict: goal is not active; resume it before sending signals or generating plans")
	case errors.Is(err, storage.ErrConflict):
		return errors.New("conflict: resource conflict, retry")
	default:
		return errors.New("internal error")
	}
}

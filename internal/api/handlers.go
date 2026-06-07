package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
)

// --- Operational -------------------------------------------------------------

// handleLive reports process liveness. It does not touch dependencies.
func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady reports readiness, including a storage ping, so load balancers only
// route traffic when the service can actually serve it.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "storage not ready")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// --- Goals -------------------------------------------------------------------

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request) {
	var req CreateGoalRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	goal, err := s.svc.CreateGoal(r.Context(), app.CreateGoalInput{
		PlayerID:  req.PlayerID,
		Domain:    req.Domain,
		Objective: req.Objective,
		Metric:    req.Metric,
		Target:    req.Target,
		Context:   req.Context,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, goal)
}

func (s *Server) handleGetGoal(w http.ResponseWriter, r *http.Request) {
	goal, err := s.svc.GetGoal(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, goal)
}

func (s *Server) handleListGoals(w http.ResponseWriter, r *http.Request) {
	goals, err := s.svc.ListGoals(r.Context(), parsePage(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": goals})
}

// --- Plans -------------------------------------------------------------------

func (s *Server) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	version, err := s.svc.GeneratePlan(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

func (s *Server) handleGetGoalPlan(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.GetGoalPlan(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plan":            view.Plan,
		"current_version": view.CurrentVersion,
	})
}

func (s *Server) handleGetPlan(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.GetPlan(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plan":            view.Plan,
		"current_version": view.CurrentVersion,
	})
}

func (s *Server) handleListPlanVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.svc.ListPlanVersions(r.Context(), chi.URLParam(r, "id"), parsePage(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// --- Signals -----------------------------------------------------------------

func (s *Server) handleCreateSignal(w http.ResponseWriter, r *http.Request) {
	var req CreateSignalRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.svc.ApplySignal(r.Context(), app.SignalInput{
		GoalID:      req.GoalID,
		Kind:        req.Kind,
		Description: req.Description,
		Payload:     req.Payload,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	// Asynchronous acceptance returns 202; synchronous (inline) replanning returns 200.
	code := http.StatusOK
	if result.Status == app.StatusPending {
		code = http.StatusAccepted
	}
	writeJSON(w, code, SignalResponse{
		Signal:      result.Signal,
		Status:      string(result.Status),
		Material:    result.Material,
		Reason:      result.Reason,
		PlanVersion: result.PlanVersion,
	})
}

// handleGetSignal returns a stored signal and its replanning status (for polling
// the outcome of an asynchronously-processed signal).
func (s *Server) handleGetSignal(w http.ResponseWriter, r *http.Request) {
	signal, err := s.svc.GetSignal(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, signal)
}

// --- Outcomes ----------------------------------------------------------------

func (s *Server) handleCreateOutcome(w http.ResponseWriter, r *http.Request) {
	var req CreateOutcomeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	outcome, err := s.svc.RecordOutcome(r.Context(), app.OutcomeInput{
		GoalID:          req.GoalID,
		PlanVersion:     req.PlanVersion,
		MoveRank:        req.MoveRank,
		Result:          req.Result,
		ObservedSignals: req.ObservedSignals,
		Notes:           req.Notes,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, outcome)
}

// --- Evaluate (stateless) ----------------------------------------------------

func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	var req EvaluateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	version, err := s.svc.Evaluate(r.Context(), app.EvaluateInput{
		Domain:     req.Domain,
		Objective:  req.Objective,
		Metric:     req.Metric,
		Target:     req.Target,
		Context:    req.Context,
		SignalNote: req.SignalNote,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

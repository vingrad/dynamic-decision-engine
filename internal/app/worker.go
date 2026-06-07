package app

import (
	"context"
	"errors"

	"github.com/vingrad/dynamic-decision-engine/internal/storage"
)

// processReplan is the ReplanQueue handler. It re-evaluates a goal's current plan
// against a signal and, if the change is material, appends a new immutable version.
//
// The version-creation step is wrapped in an optimistic-concurrency retry: if a
// concurrent replan has already written the next version (storage.ErrConflict on
// the UNIQUE(plan_id, version) constraint), it reloads the current version and
// re-evaluates against it. This serialises concurrent replans correctly. Running
// here — off the request path — means LLM latency no longer blocks signal
// ingestion, and a coalesced burst is re-evaluated against the latest state.
func (s *Service) processReplan(ctx context.Context, job ReplanJob) (ReplanOutcome, error) {
	goal, err := s.repo.GetGoal(ctx, job.GoalID)
	if err != nil {
		return s.failReplan(ctx, job, err)
	}

	for attempt := 0; attempt < maxReplanAttempts; attempt++ {
		current, err := s.repo.GetCurrentPlanVersion(ctx, job.PlanID)
		if err != nil {
			return s.failReplan(ctx, job, err)
		}

		result, err := s.eng.Replan(ctx, goal, current, job.SignalNote, job.SignalKind, job.SignalPayload)
		if err != nil {
			return s.failReplan(ctx, job, err)
		}
		s.metrics.ReplanEvaluated(goal.Domain, result.Material)

		if !result.Material {
			s.markSignal(ctx, job.SignalID, StatusUnchanged, current.Version, result.Reason, "")
			return ReplanOutcome{Processed: true, Material: false, Reason: result.Reason, Version: current}, nil
		}

		err = s.repo.CreatePlanVersion(ctx, &result.Candidate)
		switch {
		case err == nil:
			s.metrics.PlanVersionCreated(goal.Domain)
			s.markSignal(ctx, job.SignalID, StatusApplied, result.Candidate.Version, result.Reason, "")
			return ReplanOutcome{Processed: true, Material: true, Reason: result.Reason, Version: result.Candidate}, nil
		case errors.Is(err, storage.ErrConflict):
			s.log.Debug("replan version conflict, retrying", "plan_id", job.PlanID, "attempt", attempt+1)
			continue
		default:
			return s.failReplan(ctx, job, err)
		}
	}
	return s.failReplan(ctx, job, storage.ErrConflict)
}

// failReplan records a failed terminal status and the failure metric, then returns
// the error to the (async) caller.
func (s *Service) failReplan(ctx context.Context, job ReplanJob, err error) (ReplanOutcome, error) {
	s.metrics.ReplanFailed(job.Domain)
	s.markSignal(ctx, job.SignalID, StatusFailed, 0, "replan failed", err.Error())
	return ReplanOutcome{}, err
}

// markSignal persists the signal's terminal replanning status. A failure to record
// it is logged but does not change the replan result.
func (s *Service) markSignal(ctx context.Context, signalID string, status SignalStatus, version int, reason, errMsg string) {
	if err := s.repo.MarkSignalProcessed(ctx, signalID, string(status), version, reason, errMsg, s.clock()); err != nil {
		s.log.Warn("failed to record signal status", "signal_id", signalID, "status", status, "err", err)
	}
}

package app

import (
	"context"
	"errors"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/engine"
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

		result, err := s.replanWithRetry(ctx, goal, current, job)
		if err != nil {
			return s.failReplan(ctx, job, err)
		}
		s.metrics.ReplanEvaluated(goal.Domain, result.Material)

		if !result.Material {
			s.markSignal(ctx, job.SignalID, StatusUnchanged, current.Version, result.Reason, "")
			s.emitReplanCompleted(ctx, job, StatusUnchanged, result.Reason, "", &current)
			return ReplanOutcome{Processed: true, Material: false, Reason: result.Reason, Version: current}, nil
		}

		err = s.repo.CreatePlanVersion(ctx, &result.Candidate)
		switch {
		case err == nil:
			s.metrics.PlanVersionCreated(goal.Domain)
			s.markSignal(ctx, job.SignalID, StatusApplied, result.Candidate.Version, result.Reason, "")
			s.emitReplanCompleted(ctx, job, StatusApplied, result.Reason, "", &result.Candidate)
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

// replanWithRetry calls the planner, retrying transient errors up to
// s.replanRetries extra times with backoff. It stays inside processReplan and
// before failReplan, so the signal is marked failed only after retries are
// exhausted — a retried success never flips a recorded "failed" back to "applied".
// The storage-conflict retry remains the caller's outer loop.
func (s *Service) replanWithRetry(ctx context.Context, goal domain.Goal, current domain.PlanVersion, job ReplanJob) (engine.ReplanResult, error) {
	var lastErr error
	for attempt := 0; attempt <= s.replanRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return engine.ReplanResult{}, ctx.Err()
			case <-time.After(retryBackoff(attempt)):
			}
		}
		result, err := s.eng.Replan(ctx, goal, current, job.SignalNote, job.SignalKind, job.SignalPayload)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isTransientReplanErr(err) {
			return engine.ReplanResult{}, err
		}
		s.log.Debug("transient replan error, retrying", "signal_id", job.SignalID, "attempt", attempt+1, "err", err)
	}
	return engine.ReplanResult{}, lastErr
}

// isTransientReplanErr reports whether a planner error is worth retrying. Invalid
// input (terminal) and context cancellation/timeout (the job deadline fired) are
// not retried; anything else is treated as a transient planner/transport failure.
func isTransientReplanErr(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var ve *ValidationError
	return !errors.As(err, &ve)
}

// retryBackoff is a small exponential backoff (50ms, 100ms, 200ms, … capped at 2s).
func retryBackoff(attempt int) time.Duration {
	d := 50 * time.Millisecond << (attempt - 1)
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	return d
}

// failReplan records a failed terminal status and the failure metric, then returns
// the error to the (async) caller.
func (s *Service) failReplan(ctx context.Context, job ReplanJob, err error) (ReplanOutcome, error) {
	s.metrics.ReplanFailed(job.Domain)
	s.markSignal(ctx, job.SignalID, StatusFailed, 0, "replan failed", err.Error())
	s.emitReplanCompleted(ctx, job, StatusFailed, "replan failed", err.Error(), nil)
	return ReplanOutcome{}, err
}

// emitReplanCompleted emits the terminal replan.completed event for a job. The
// context is detached from cancellation so a job-deadline expiry (which is itself
// a reportable outcome) does not suppress the notification.
func (s *Service) emitReplanCompleted(ctx context.Context, job ReplanJob, status SignalStatus, reason, errMsg string, version *domain.PlanVersion) {
	s.emit(context.WithoutCancel(ctx), EventReplanCompleted, ReplanCompletedPayload{
		GoalID:   job.GoalID,
		PlanID:   job.PlanID,
		SignalID: job.SignalID,
		Status:   status,
		Reason:   reason,
		Error:    errMsg,
		Version:  version,
	})
}

// markSignal persists the signal's terminal replanning status. A failure to record
// it is logged but does not change the replan result.
func (s *Service) markSignal(ctx context.Context, signalID string, status SignalStatus, version int, reason, errMsg string) {
	if err := s.repo.MarkSignalProcessed(ctx, signalID, string(status), version, reason, errMsg, s.clock()); err != nil {
		s.log.Warn("failed to record signal status", "signal_id", signalID, "status", status, "err", err)
	}
}

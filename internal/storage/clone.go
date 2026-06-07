package storage

import "github.com/vingrad/dynamic-decision-engine/internal/domain"

// Deep-copy helpers used by the in-memory store so that stored entities cannot be
// mutated through references held by callers. They mirror the immutability the
// SQL store gets for free.

func clonePlayer(p domain.Player) domain.Player {
	if p.Metadata != nil {
		md := make(map[string]string, len(p.Metadata))
		for k, v := range p.Metadata {
			md[k] = v
		}
		p.Metadata = md
	}
	return p
}

func cloneGoal(g domain.Goal) domain.Goal {
	g.Context = cloneContext(g.Context)
	if g.Resolution != nil {
		r := *g.Resolution
		g.Resolution = &r
	}
	return g
}

func cloneContext(c domain.Context) domain.Context {
	c.Facts = cloneStrings(c.Facts)
	if c.Assets != nil {
		assets := make([]domain.Asset, len(c.Assets))
		copy(assets, c.Assets)
		c.Assets = assets
	}
	if c.Constraints != nil {
		cons := make([]domain.Constraint, len(c.Constraints))
		copy(cons, c.Constraints)
		c.Constraints = cons
	}
	return c
}

func clonePlanVersion(v domain.PlanVersion) domain.PlanVersion {
	if v.RankedMoves != nil {
		moves := make([]domain.RankedMove, len(v.RankedMoves))
		for i, mv := range v.RankedMoves {
			mv.FallbackMoves = cloneStrings(mv.FallbackMoves)
			mv.DependsOn = cloneStrings(mv.DependsOn)
			mv.Experiment.SuccessSignals = cloneStrings(mv.Experiment.SuccessSignals)
			mv.Experiment.KillCriteria = cloneStrings(mv.Experiment.KillCriteria)
			moves[i] = mv
		}
		v.RankedMoves = moves
	}
	return v
}

func cloneSignal(s domain.Signal) domain.Signal {
	if s.Payload != nil {
		pl := make(map[string]any, len(s.Payload))
		for k, v := range s.Payload {
			pl[k] = v
		}
		s.Payload = pl
	}
	if s.ProcessedAt != nil {
		t := *s.ProcessedAt
		s.ProcessedAt = &t
	}
	return s
}

func cloneOutcome(o domain.Outcome) domain.Outcome {
	o.ObservedSignals = cloneStrings(o.ObservedSignals)
	return o
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// snapshotInput is the subset of a planning request that actually influences the
// decision. Hashing only these fields (not IDs or timestamps) keeps the snapshot
// id stable across runs for equivalent decisions.
type snapshotInput struct {
	Objective  string         `json:"objective"`
	Metric     string         `json:"metric"`
	Target     string         `json:"target"`
	Context    domain.Context `json:"context"`
	SignalNote string         `json:"signal_note"`
}

// inputSnapshotID returns a stable, audit-friendly fingerprint of the exact
// inputs a plan was reasoned over. Shared by every planner implementation so
// provenance is consistent regardless of backend.
func inputSnapshotID(g domain.Goal, signalNote string) string {
	snap := snapshotInput{
		Objective:  g.Objective,
		Metric:     g.Metric,
		Target:     g.Target,
		Context:    g.Context,
		SignalNote: signalNote,
	}
	b, _ := json.Marshal(snap)
	sum := sha256.Sum256(b)
	return "snap_" + hex.EncodeToString(sum[:])[:16]
}

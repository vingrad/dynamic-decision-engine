package storage

import "errors"

// Sentinel errors returned by all Repository implementations so that callers can
// react uniformly regardless of the backing store.
var (
	// ErrNotFound indicates the requested entity does not exist.
	ErrNotFound = errors.New("storage: not found")
	// ErrConflict indicates a uniqueness/immutability violation, e.g. trying to
	// create a plan version that already exists.
	ErrConflict = errors.New("storage: conflict")
)

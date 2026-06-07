package storage

import (
	"context"
	"log/slog"
)

// Options configures which storage backend to open.
type Options struct {
	// DatabaseURL is a PostgreSQL connection string. When empty, the in-memory
	// store is used instead, so the system runs with zero infrastructure.
	DatabaseURL string
	// MaxConns bounds the PostgreSQL connection pool. Ignored for the memory store.
	MaxConns int32
}

// Open returns a Repository for the given options: PostgreSQL when a database URL
// is provided (running migrations on connect), otherwise the in-memory store.
func Open(ctx context.Context, opts Options, log *slog.Logger) (Repository, error) {
	if opts.DatabaseURL == "" {
		log.Info("using in-memory storage (no DATABASE_URL configured)")
		return NewMemory(), nil
	}
	log.Info("using postgres storage")
	return NewPostgres(ctx, opts, log)
}

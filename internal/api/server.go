// Package api exposes the decision engine over a versioned REST interface. It
// owns transport concerns only — routing, (de)serialisation, validation,
// observability — and delegates all reasoning to the engine and all persistence
// to the storage layer.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/app"
	"github.com/vingrad/dynamic-decision-engine/internal/config"
)

// Server wires together the dependencies needed to serve the API. It owns
// transport concerns only and delegates every use-case to the app service.
type Server struct {
	cfg     config.Config
	log     *slog.Logger
	svc     *app.Service
	metrics *Metrics
	mcp     http.Handler
}

// ServerOption customises a Server.
type ServerOption func(*Server)

// WithMCP mounts h at /mcp. The handler is opaque to this package (an MCP
// streamable-HTTP handler in practice), keeping the api package free of any
// MCP SDK dependency.
func WithMCP(h http.Handler) ServerOption {
	return func(s *Server) { s.mcp = h }
}

// New constructs a Server around the application service. The same *Metrics
// passed here should also be wired into the service so counters and the /metrics
// endpoint share one registry.
func New(cfg config.Config, log *slog.Logger, svc *app.Service, metrics *Metrics, opts ...ServerOption) *Server {
	s := &Server{
		cfg:     cfg,
		log:     log,
		svc:     svc,
		metrics: metrics,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run starts the HTTP server and blocks until ctx is cancelled, then performs a
// graceful shutdown that drains in-flight requests.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.HTTPAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       s.cfg.RequestTimeout + 5*time.Second,
		// No connection-level write deadline: /mcp streams long-lived SSE
		// responses. The REST routes are still bounded per request by the
		// timeout middleware (see Handler).
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", "addr", s.cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("shutting down http server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

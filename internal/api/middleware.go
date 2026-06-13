package api

import (
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/vingrad/dynamic-decision-engine/internal/llm"
)

// requestLogger logs one structured line per request with method, path, status,
// duration and the request ID, using the application's slog logger.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		s.log.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

// corsMiddleware applies a minimal, configurable CORS policy so the separate
// admin UI origin can call the API from the browser. Auth is intentionally out
// of scope for this iteration; this is the seam where it would be added.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	allowed := s.cfg.CORSAllowedOrigins
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (slices.Contains(allowed, "*") || slices.Contains(allowed, origin)) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			// X-LLM-Provider / X-LLM-Key carry per-request bring-your-own-key
			// credentials (see llmCredentialsMiddleware).
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-LLM-Provider, X-LLM-Key")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// llmCredentialsMiddleware reads optional bring-your-own-key headers
// (X-LLM-Provider, X-LLM-Key) and stashes them on the request context, where the
// byok planner picks them up. It is harmless when no byok planner is active: the
// values are simply never read. The key is never logged.
func (s *Server) llmCredentialsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key := r.Header.Get("X-LLM-Key"); key != "" {
			ctx := llm.WithCredentials(r.Context(), llm.Credentials{
				Provider: r.Header.Get("X-LLM-Provider"),
				Key:      key,
			})
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

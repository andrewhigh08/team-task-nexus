package middleware

import (
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// Logging stores log in the request context together with the id assigned by chi's RequestID
// middleware, so that everything logged through logger.FromContext while serving the request
// carries the id, and logs every request once it has been served.
func Logging(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := logger.NewContext(r.Context(), log)
			ctx = logger.WithRequestIDContext(ctx, chimiddleware.GetReqID(ctx))
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r.WithContext(ctx))

			logger.FromContext(ctx).Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

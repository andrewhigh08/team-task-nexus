package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// probeRoutes are polled by orchestrators and Prometheus every few seconds. Their requests
// are logged at debug level, so that they do not drown out the requests of clients.
var probeRoutes = map[string]bool{
	"/health/live":  true,
	"/health/ready": true,
	"/metrics":      true,
}

// accessEntry collects what the access log reports but only a middleware further down
// learns: the authenticated user, which WithUserID records.
type accessEntry struct {
	userID int64
}

type accessEntryKey struct{}

// AccessLog stores log in the request context, where logger.FromContext finds it for
// everything that serves the request, and logs every request once it has been served: method,
// route pattern, status, response size, duration, request id and the authenticated user if
// there is one. Probes are logged at debug level, every other request at info.
func AccessLog(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			entry := &accessEntry{}
			ctx := context.WithValue(logger.NewContext(r.Context(), log), accessEntryKey{}, entry)
			rec := newResponseRecorder(w)
			completed := false
			// Deferred, so that a request whose handler panics is logged too, with the status
			// Recoverer is about to answer it with.
			defer func() {
				logAccess(log, r, rec, completed, entry, time.Since(start))
			}()
			next.ServeHTTP(rec, r.WithContext(ctx))
			completed = true
		})
	}
}

func logAccess(
	log *logger.Logger, r *http.Request, rec *responseRecorder, completed bool, entry *accessEntry, elapsed time.Duration,
) {
	route := routeLabel(r)
	attrs := []slog.Attr{
		slog.String("method", r.Method),
		slog.String("route", route),
		slog.Int("status", rec.finalStatus(completed)),
		slog.Int("bytes", rec.bytes),
		slog.Float64("duration_ms", float64(elapsed.Microseconds())/1e3),
		slog.String("request_id", logger.RequestIDFromContext(r.Context())),
	}
	if entry.userID != 0 {
		attrs = append(attrs, slog.Int64("user_id", entry.userID))
	}
	level := slog.LevelInfo
	if probeRoutes[route] {
		level = slog.LevelDebug
	}
	log.LogAttrs(r.Context(), level, "http request", attrs...)
}

// recordUser reports the authenticated user to the access log of the request in ctx, if any.
func recordUser(ctx context.Context, userID int64) {
	if entry, ok := ctx.Value(accessEntryKey{}).(*accessEntry); ok {
		entry.userID = userID
	}
}

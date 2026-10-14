package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// Recoverer turns a panic while serving a request into a 500: it logs the panic value and the
// stack with the request id and, unless the response had already been started, writes the
// INTERNAL_ERROR envelope. A panic with http.ErrAbortHandler is passed on, since it asks
// net/http to abort the response.
func Recoverer(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := newResponseRecorder(w)
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}
				log.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.String("request_id", logger.RequestIDFromContext(r.Context())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("panic", fmt.Sprint(recovered)),
					slog.String("stack", string(debug.Stack())),
				)
				if !rec.written() {
					response.InternalError(w)
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

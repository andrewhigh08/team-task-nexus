package middleware

import (
	"net/http"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// retryAfterSeconds is the window of the limiter: a client that waits this long is admitted again.
const retryAfterSeconds = 60

// RateLimit rejects requests of authenticated users above the limit enforced by limiter.
// Requests are let through when the limiter fails.
func RateLimit(limiter port.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := GetUserID(r.Context())
			if userID == 0 {
				next.ServeHTTP(w, r)
				return
			}

			allowed, err := limiter.Allow(r.Context(), userID)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if !allowed {
				response.Error(w, r, apperror.TooManyRequests("rate limit exceeded", retryAfterSeconds))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

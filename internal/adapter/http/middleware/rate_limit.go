package middleware

import (
	"net/http"

	"github.com/shalfey088/team-task-nexus/internal/adapter/http/response"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/internal/port"
)

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
				response.Error(w, apperror.ErrRateLimited)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

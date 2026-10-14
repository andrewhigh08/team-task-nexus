package http_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	apphttp "github.com/andrewhigh08/team-task-nexus/internal/adapter/http"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/handler"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

const (
	testAPIRateLimit  = 100
	testAuthRateLimit = 10
	testToken         = "header.payload.signature"
)

// TestNewRouter_RateLimits checks which limit guards which route. The limiter rejects every
// request it is asked about, so no handler runs and the services need no expectations.
func TestNewRouter_RateLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		method        string
		path          string
		authorization string
		forwardedFor  string
		trustProxy    bool
		wantKey       string // empty: the limiter is not consulted
		wantLimit     int
		wantStatus    int
	}{
		{
			name: "registration is limited per client address", method: http.MethodPost, path: "/api/v1/register",
			forwardedFor: "198.51.100.1", wantKey: "rl:auth:ip:203.0.113.7", wantLimit: testAuthRateLimit,
			wantStatus: http.StatusTooManyRequests,
		},
		{
			name: "login is limited per client address", method: http.MethodPost, path: "/api/v1/login",
			forwardedFor: "198.51.100.1", wantKey: "rl:auth:ip:203.0.113.7", wantLimit: testAuthRateLimit,
			wantStatus: http.StatusTooManyRequests,
		},
		{
			name: "login behind a trusted proxy", method: http.MethodPost, path: "/api/v1/login",
			forwardedFor: "198.51.100.1", trustProxy: true, wantKey: "rl:auth:ip:198.51.100.1", wantLimit: testAuthRateLimit,
			wantStatus: http.StatusTooManyRequests,
		},
		{
			name: "authenticated requests are limited per user", method: http.MethodGet, path: "/api/v1/tasks",
			authorization: "Bearer " + testToken, wantKey: "rl:api:user:7", wantLimit: testAPIRateLimit,
			wantStatus: http.StatusTooManyRequests,
		},
		{
			name: "unauthenticated requests are rejected before the limiter", method: http.MethodGet, path: "/api/v1/teams",
			wantStatus: http.StatusUnauthorized,
		},
		{name: "health check is not limited", method: http.MethodGet, path: "/health", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			limiter := mocks.NewMockRateLimiter(ctrl)
			if tt.wantKey != "" {
				limiter.EXPECT().Allow(gomock.Any(), tt.wantKey, tt.wantLimit, time.Minute).
					Return(port.RateDecision{RetryAfter: 30 * time.Second}, nil)
			}
			verifier := mocks.NewMockTokenVerifier(ctrl)
			if tt.authorization != "" {
				verifier.EXPECT().Verify(testToken).Return(int64(7), nil)
			}
			router := apphttp.NewRouter(apphttp.RouterDeps{
				AuthHandler:       handler.NewAuthHandler(mocks.NewMockAuthService(ctrl)),
				TeamHandler:       handler.NewTeamHandler(mocks.NewMockTeamService(ctrl)),
				TaskHandler:       handler.NewTaskHandler(mocks.NewMockTaskService(ctrl)),
				CommentHandler:    handler.NewCommentHandler(mocks.NewMockCommentService(ctrl)),
				HealthHandler:     handler.NewHealthHandler(),
				TokenVerifier:     verifier,
				RateLimiter:       limiter,
				Logger:            logger.New(logger.Config{Output: io.Discard}),
				APIRateLimit:      testAPIRateLimit,
				AuthRateLimit:     testAuthRateLimit,
				TrustProxyHeaders: tt.trustProxy,
			})
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, http.NoBody)
			req.RemoteAddr = "203.0.113.7:4711"
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			if tt.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantKey == "" {
				assert.Empty(t, rec.Header().Get("X-RateLimit-Limit"))
			}
		})
	}
}

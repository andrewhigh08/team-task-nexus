package middleware_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/metrics"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

const testPerMinute = 42

var errLimiterDown = errors.New("redis down")

// limitedRequest runs a request through limit and reports whether it reached the handler.
func limitedRequest(
	t *testing.T, limit func(http.Handler) http.Handler, req *http.Request,
) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	var reached bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	limit(next).ServeHTTP(rec, req)
	return rec, reached
}

func TestRateLimitByUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		decision       port.RateDecision
		limiterErr     error
		wantStatus     int
		wantRemaining  string
		wantRetryAfter string
		wantBody       string
	}{
		{
			name: "allowed", decision: port.RateDecision{Allowed: true, Remaining: 41},
			wantStatus: http.StatusNoContent, wantRemaining: "41",
		},
		{
			name: "limited", decision: port.RateDecision{RetryAfter: 12300 * time.Millisecond},
			wantStatus: http.StatusTooManyRequests, wantRemaining: "0", wantRetryAfter: "13",
			wantBody: `{"success":false,"error":{"code":"TOO_MANY_REQUESTS","message":"rate limit exceeded","details":{"retry_after_seconds":13}}}`,
		},
		{
			name: "a wait below one second is announced as one second", decision: port.RateDecision{RetryAfter: 200 * time.Millisecond},
			wantStatus: http.StatusTooManyRequests, wantRemaining: "0", wantRetryAfter: "1",
			wantBody: `{"success":false,"error":{"code":"TOO_MANY_REQUESTS","message":"rate limit exceeded","details":{"retry_after_seconds":1}}}`,
		},
		{name: "limiter failure lets the request through", limiterErr: errLimiterDown, wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limiter := mocks.NewMockRateLimiter(gomock.NewController(t))
			limiter.EXPECT().Allow(gomock.Any(), "rl:api:user:7", testPerMinute, time.Minute).Return(tt.decision, tt.limiterErr)
			var logs bytes.Buffer
			ctx := logger.NewContext(t.Context(), logger.New(logger.Config{Format: "json", Output: &logs}))
			req := httptest.NewRequestWithContext(middleware.WithUserID(ctx, 7), http.MethodGet, "/api/v1/tasks", http.NoBody)

			rec, reached := limitedRequest(t, middleware.RateLimitByUser(limiter, testPerMinute), req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantStatus == http.StatusNoContent, reached, "only admitted requests reach the handler")
			assert.Equal(t, "42", rec.Header().Get("X-RateLimit-Limit"))
			assert.Equal(t, tt.wantRemaining, rec.Header().Get("X-RateLimit-Remaining"))
			assert.Equal(t, tt.wantRetryAfter, rec.Header().Get("Retry-After"))
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, rec.Body.String())
			}
			if tt.limiterErr == nil {
				assert.Empty(t, logs.String())
				return
			}
			records := logRecords(t, &logs)
			require.Len(t, records, 1)
			assert.Equal(t, "WARN", records[0]["level"])
			assert.Equal(t, "rate limiter failed, request let through", records[0]["msg"])
			assert.Equal(t, metrics.ScopeAPI, records[0]["scope"])
			assert.Equal(t, errLimiterDown.Error(), records[0]["error"])
			assert.InDelta(t, 7, records[0]["user_id"], 0)
		})
	}
}

func TestRateLimitByUser_RejectsRequestsWithoutAUser(t *testing.T) {
	t.Parallel()
	limiter := mocks.NewMockRateLimiter(gomock.NewController(t))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", http.NoBody)

	rec, reached := limitedRequest(t, middleware.RateLimitByUser(limiter, testPerMinute), req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, reached)
	assert.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
}

func TestRateLimitByIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		remoteAddr   string
		forwardedFor string
		trustProxy   bool
		wantKey      string
	}{
		{name: "ipv4 client", remoteAddr: "203.0.113.7:4711", wantKey: "rl:auth:ip:203.0.113.7"},
		{name: "ipv6 client is keyed by its /64", remoteAddr: "[2001:db8:1:2:3:4:5:6]:4711", wantKey: "rl:auth:ip:2001:db8:1:2::/64"},
		{
			name: "ipv6 host bits do not matter", remoteAddr: "[2001:db8:1:2:ffff:ffff:ffff:ffff]:4711",
			wantKey: "rl:auth:ip:2001:db8:1:2::/64",
		},
		{name: "ipv4-mapped client is keyed as ipv4", remoteAddr: "[::ffff:203.0.113.7]:4711", wantKey: "rl:auth:ip:203.0.113.7"},
		{name: "unparsable clients share one window", remoteAddr: "pipe", wantKey: "rl:auth:ip:unknown"},
		{
			name: "forwarded address is ignored unless trusted", remoteAddr: "203.0.113.7:4711", forwardedFor: "198.51.100.1",
			wantKey: "rl:auth:ip:203.0.113.7",
		},
		{
			name: "forwarded address behind a trusted proxy", remoteAddr: "10.0.0.2:4711", forwardedFor: "198.51.100.1, 10.0.0.1",
			trustProxy: true, wantKey: "rl:auth:ip:198.51.100.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limiter := mocks.NewMockRateLimiter(gomock.NewController(t))
			limiter.EXPECT().Allow(gomock.Any(), tt.wantKey, 5, time.Minute).
				Return(port.RateDecision{Allowed: true, Remaining: 4}, nil)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/login", http.NoBody)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}

			rec, reached := limitedRequest(t, middleware.RateLimitByIP(limiter, 5, tt.trustProxy), req)

			assert.Equal(t, http.StatusNoContent, rec.Code)
			assert.True(t, reached)
			assert.Equal(t, "5", rec.Header().Get("X-RateLimit-Limit"))
			assert.Equal(t, "4", rec.Header().Get("X-RateLimit-Remaining"))
		})
	}
}

// TestRateLimit_CountsDecisions does not run in parallel: the counters are process-wide, and
// a parallel test passing the same middleware would skew the deltas asserted here.
func TestRateLimit_CountsDecisions(t *testing.T) {
	tests := []struct {
		name       string
		byIP       bool
		decision   port.RateDecision
		limiterErr error
		wantScope  string
		wantResult string
	}{
		{name: "api allowed", decision: port.RateDecision{Allowed: true}, wantScope: metrics.ScopeAPI, wantResult: metrics.ResultAllowed},
		{name: "api limited", decision: port.RateDecision{RetryAfter: time.Second}, wantScope: metrics.ScopeAPI, wantResult: metrics.ResultLimited},
		{name: "api error", limiterErr: errLimiterDown, wantScope: metrics.ScopeAPI, wantResult: metrics.ResultError},
		{name: "auth allowed", byIP: true, decision: port.RateDecision{Allowed: true}, wantScope: metrics.ScopeAuth, wantResult: metrics.ResultAllowed},
		{name: "auth limited", byIP: true, decision: port.RateDecision{RetryAfter: time.Second}, wantScope: metrics.ScopeAuth, wantResult: metrics.ResultLimited},
		{name: "auth error", byIP: true, limiterErr: errLimiterDown, wantScope: metrics.ScopeAuth, wantResult: metrics.ResultError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := mocks.NewMockRateLimiter(gomock.NewController(t))
			limiter.EXPECT().Allow(gomock.Any(), gomock.Any(), testPerMinute, time.Minute).Return(tt.decision, tt.limiterErr)
			limit := middleware.RateLimitByUser(limiter, testPerMinute)
			if tt.byIP {
				limit = middleware.RateLimitByIP(limiter, testPerMinute, false)
			}
			ctx := logger.NewContext(middleware.WithUserID(t.Context(), 7), logger.New(logger.Config{Output: io.Discard}))
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", http.NoBody)
			want := decisionCounts()
			want[tt.wantScope+"/"+tt.wantResult]++

			limitedRequest(t, limit, req)

			assert.Equal(t, want, decisionCounts(), "exactly one series is incremented")
		})
	}
}

// decisionCounts returns every series of the rate limit counter keyed by "scope/result".
func decisionCounts() map[string]float64 {
	counts := make(map[string]float64)
	for _, scope := range []string{metrics.ScopeAPI, metrics.ScopeAuth} {
		for _, result := range []string{metrics.ResultAllowed, metrics.ResultLimited, metrics.ResultError} {
			counts[scope+"/"+result] = testutil.ToFloat64(metrics.RateLimitDecisions.WithLabelValues(scope, result))
		}
	}
	return counts
}

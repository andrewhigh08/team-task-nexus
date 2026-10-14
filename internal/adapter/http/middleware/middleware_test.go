package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for line := range strings.Lines(buf.String()) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "log line: %s", line)
		records = append(records, rec)
	}
	return records
}

func signedToken(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}

func TestLogging(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantLogs   []string
	}{
		{
			name:       "success",
			handler:    func(w http.ResponseWriter, _ *http.Request) { response.JSON(w, http.StatusOK, "ok") },
			wantStatus: http.StatusOK,
			wantLogs:   []string{"http request"},
		},
		{
			name:       "server error is logged with the request id",
			handler:    func(w http.ResponseWriter, r *http.Request) { response.Error(w, r, errors.New("boom")) },
			wantStatus: http.StatusInternalServerError,
			wantLogs:   []string{"request failed", "http request"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			log := logger.New(logger.Config{Format: "json", Output: &logs})
			h := chimiddleware.RequestID(middleware.Logging(log)(tt.handler))
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks?page=2", http.NoBody)
			req.Header.Set(chimiddleware.RequestIDHeader, "req-42")
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			records := logRecords(t, &logs)
			require.Len(t, records, len(tt.wantLogs))
			for i, msg := range tt.wantLogs {
				assert.Equal(t, msg, records[i]["msg"])
				assert.Equal(t, "req-42", records[i]["request_id"], "record %q carries the request id", msg)
			}
			access := records[len(records)-1]
			assert.Equal(t, http.MethodGet, access["method"])
			assert.Equal(t, "/api/v1/tasks", access["path"])
			assert.InDelta(t, tt.wantStatus, access["status"], 0)
			assert.Contains(t, access, "duration_ms")
		})
	}
}

func TestJWTAuth(t *testing.T) {
	t.Parallel()
	valid := signedToken(t, testSecret, jwt.MapClaims{"user_id": 7, "exp": time.Now().Add(time.Hour).Unix()})
	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantUserID int64
	}{
		{name: "valid token", header: "Bearer " + valid, wantStatus: http.StatusNoContent, wantUserID: 7},
		{name: "missing header", header: "", wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Basic " + valid, wantStatus: http.StatusUnauthorized},
		{
			name:       "foreign signature",
			header:     "Bearer " + signedToken(t, strings.Repeat("x", 32), jwt.MapClaims{"user_id": 7}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "expired token",
			header:     "Bearer " + signedToken(t, testSecret, jwt.MapClaims{"user_id": 7, "exp": time.Now().Add(-time.Hour).Unix()}),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "token without a user",
			header:     "Bearer " + signedToken(t, testSecret, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}),
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotUserID, gotLogUserID int64
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUserID = middleware.GetUserID(r.Context())
				gotLogUserID = logger.UserIDFromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/teams", http.NoBody)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()

			middleware.JWTAuth(testSecret)(next).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantUserID, gotUserID)
			assert.Equal(t, tt.wantUserID, gotLogUserID, "the user id reaches the request logger")
			if tt.wantStatus == http.StatusUnauthorized {
				assert.JSONEq(t, `{"success":false,"error":{"code":"UNAUTHORIZED","message":"authentication required"}}`, rec.Body.String())
			}
		})
	}
}

func TestRateLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		userID     int64
		allowed    bool
		limiterErr error
		wantStatus int
		wantBody   string
	}{
		{name: "anonymous requests are not limited", wantStatus: http.StatusNoContent},
		{name: "allowed", userID: 7, allowed: true, wantStatus: http.StatusNoContent},
		{
			name: "denied", userID: 7, allowed: false, wantStatus: http.StatusTooManyRequests,
			wantBody: `{"success":false,"error":{"code":"TOO_MANY_REQUESTS","message":"rate limit exceeded","details":{"retry_after_seconds":60}}}`,
		},
		{name: "limiter failure lets the request through", userID: 7, limiterErr: errors.New("redis down"), wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limiter := new(mocks.RateLimiterMock)
			if tt.userID != 0 {
				limiter.On("Allow", mock.Anything, tt.userID).Return(tt.allowed, tt.limiterErr).Once()
			}
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
			ctx := t.Context()
			if tt.userID != 0 {
				ctx = context.WithValue(ctx, middleware.UserIDKey, tt.userID)
			}
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/tasks", http.NoBody)
			rec := httptest.NewRecorder()

			middleware.RateLimit(limiter)(next).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, rec.Body.String())
			}
			limiter.AssertExpectations(t)
		})
	}
}

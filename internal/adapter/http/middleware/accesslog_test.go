package middleware_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

// accessLogRouter serves a few routes behind RequestID and AccessLog; creating a task requires
// authentication by verifier.
func accessLogRouter(log *logger.Logger, verifier *mocks.MockTokenVerifier) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.AccessLog(log))
	r.With(middleware.Authenticate(verifier)).Post("/api/v1/tasks/{id}/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})
	r.Get("/api/v1/teams", func(http.ResponseWriter, *http.Request) {})
	r.Get("/api/v1/fail", func(w http.ResponseWriter, r *http.Request) { response.Error(w, r, errors.New("boom")) })
	for _, probe := range []string{"/health/live", "/health/ready", "/metrics"} {
		r.Get(probe, noContent)
	}
	return r
}

func TestAccessLog(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		method        string
		target        string
		authorization string
		wantStatus    int
		wantRoute     string
		wantBytes     int
		wantUserID    int64 // 0: no user is logged
		wantLevel     string
	}{
		{
			name: "authenticated request", method: http.MethodPost, target: "/api/v1/tasks/7/comments", authorization: "Bearer " + testToken,
			wantStatus: http.StatusCreated, wantRoute: "/api/v1/tasks/{id}/comments", wantBytes: len("created"), wantUserID: testUserID,
			wantLevel: "INFO",
		},
		{
			name: "rejected request", method: http.MethodPost, target: "/api/v1/tasks/7/comments",
			wantStatus: http.StatusUnauthorized, wantRoute: "/api/v1/tasks/{id}/comments",
			wantBytes: len(`{"success":false,"error":{"code":"UNAUTHORIZED","message":"authentication required"}}` + "\n"), wantLevel: "INFO",
		},
		{
			name: "handler that writes nothing", method: http.MethodGet, target: "/api/v1/teams",
			wantStatus: http.StatusOK, wantRoute: "/api/v1/teams", wantLevel: "INFO",
		},
		{
			name: "unmatched route", method: http.MethodGet, target: "/api/v1/nothing/here",
			wantStatus: http.StatusNotFound, wantRoute: "unmatched", wantBytes: len("404 page not found\n"), wantLevel: "INFO",
		},
		{name: "liveness probe", method: http.MethodGet, target: "/health/live", wantStatus: http.StatusNoContent, wantRoute: "/health/live", wantLevel: "DEBUG"},
		{name: "readiness probe", method: http.MethodGet, target: "/health/ready", wantStatus: http.StatusNoContent, wantRoute: "/health/ready", wantLevel: "DEBUG"},
		{name: "metrics scrape", method: http.MethodGet, target: "/metrics", wantStatus: http.StatusNoContent, wantRoute: "/metrics", wantLevel: "DEBUG"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			verifier := mocks.NewMockTokenVerifier(gomock.NewController(t))
			if tt.authorization != "" {
				verifier.EXPECT().Verify(testToken).Return(testUserID, nil)
			}
			var logs bytes.Buffer
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, http.NoBody)
			req.Header.Set(middleware.RequestIDHeader, "req-1")
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}

			rec := serve(accessLogRouter(newTestLogger(&logs), verifier), req)

			require.Equal(t, tt.wantStatus, rec.Code)
			records := logRecords(t, &logs)
			require.Len(t, records, 1)
			entry := records[0]
			assert.Equal(t, tt.wantLevel, entry["level"])
			assert.Equal(t, "http request", entry["msg"])
			assert.Equal(t, tt.method, entry["method"])
			assert.Equal(t, tt.wantRoute, entry["route"], "the route pattern is logged, never the raw path")
			assert.InDelta(t, tt.wantStatus, entry["status"], 0)
			assert.InDelta(t, tt.wantBytes, entry["bytes"], 0)
			assert.Equal(t, "req-1", entry["request_id"])
			duration, ok := entry["duration_ms"].(float64)
			require.True(t, ok, "duration_ms is a number")
			assert.GreaterOrEqual(t, duration, 0.0)
			if tt.wantUserID == 0 {
				assert.NotContains(t, entry, "user_id")
				return
			}
			assert.InDelta(t, tt.wantUserID, entry["user_id"], 0, "the user authenticated further down is logged")
		})
	}
}

func TestAccessLog_HandsItsLoggerToTheHandlers(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/fail", http.NoBody)
	req.Header.Set(middleware.RequestIDHeader, "req-2")

	rec := serve(accessLogRouter(newTestLogger(&logs), nil), req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	records := logRecords(t, &logs)
	require.Len(t, records, 2)
	assert.Equal(t, "request failed", records[0]["msg"], "the handler logs through the logger of the access log")
	assert.Equal(t, "req-2", records[0]["request_id"])
	assert.Equal(t, "boom", records[0]["error"])
	assert.Equal(t, "http request", records[1]["msg"])
	assert.InDelta(t, http.StatusInternalServerError, records[1]["status"], 0)
}

func TestAccessLog_LogsAPanickingRequestAsRecovered(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	log := newTestLogger(&logs)
	r := chi.NewRouter()
	r.Use(middleware.Recoverer(log), middleware.AccessLog(log))
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })

	rec := serve(r, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/boom", http.NoBody))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	records := logRecords(t, &logs)
	require.Len(t, records, 2)
	assert.Equal(t, "http request", records[0]["msg"])
	assert.Equal(t, "/boom", records[0]["route"])
	assert.InDelta(t, http.StatusInternalServerError, records[0]["status"], 0, "logged with the status Recoverer answers")
	assert.Equal(t, "panic recovered", records[1]["msg"])
}

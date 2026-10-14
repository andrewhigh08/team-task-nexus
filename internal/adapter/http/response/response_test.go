package response

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// newRequest returns a request whose context carries a JSON logger writing to logs and a request id.
func newRequest(t *testing.T, logs *bytes.Buffer) *http.Request {
	t.Helper()
	ctx := logger.NewContext(t.Context(), logger.New(logger.Config{Format: "json", Output: logs}))
	ctx = logger.WithRequestIDContext(ctx, "req-1")
	return httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/tasks", http.NoBody)
}

func TestJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		data   any
		want   string
	}{
		{name: "object", status: http.StatusOK, data: map[string]int{"id": 7}, want: `{"success":true,"data":{"id":7}}`},
		{name: "created", status: http.StatusCreated, data: []string{"a"}, want: `{"success":true,"data":["a"]}`},
		{name: "no data", status: http.StatusOK, data: nil, want: `{"success":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()

			JSON(rec, tt.status, tt.data)

			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.JSONEq(t, tt.want, rec.Body.String())
		})
	}
}

func TestJSONWithMeta(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()

	JSONWithMeta(rec, []int{}, Meta{Page: 3, PageSize: 20, Total: 41, TotalPages: 3})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"success":true,"data":[],"meta":{"page":3,"page_size":20,"total":41,"total_pages":3}}`, rec.Body.String())
}

func TestJSON_UnencodablePayload(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()

	JSON(rec, http.StatusOK, map[string]any{"fn": func() {}})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`, rec.Body.String())
}

func TestInternalError(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()

	InternalError(rec)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`, rec.Body.String())
}

func TestPlain(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()

	Plain(rec, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status":"not_ready"}`, rec.Body.String(), "the body is not wrapped in the envelope")
}

func TestError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		err     error
		status  int
		want    string
		logged  bool
		leakage string
	}{
		{
			name: "client error keeps its code, message and details", err: apperror.NotFound("task", 42), status: http.StatusNotFound,
			want: `{"success":false,"error":{"code":"NOT_FOUND","message":"task not found","details":{"resource":"task","id":42}}}`,
		},
		{
			name: "wrapped app error is unwrapped", err: fmt.Errorf("update task: %w", apperror.Forbidden("")), status: http.StatusForbidden,
			want: `{"success":false,"error":{"code":"FORBIDDEN","message":"access denied"}}`,
		},
		{
			name: "plain error becomes a generic 500", err: errors.New("dial tcp 10.0.0.5:3306: connection refused"),
			status: http.StatusInternalServerError, logged: true, leakage: "10.0.0.5",
			want: `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`,
		},
		{
			name: "nil error becomes a generic 500", err: nil, status: http.StatusInternalServerError, logged: true,
			want: `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`,
		},
		{
			name: "internal error hides its operation and cause", err: apperror.Internal("list tasks", errors.New("password=hunter2")),
			status: http.StatusInternalServerError, logged: true, leakage: "hunter2",
			want: `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`,
		},
		{
			name: "other server errors keep their message", err: &apperror.AppError{Code: "BAD_GATEWAY", Message: "upstream is down", HTTPStatus: http.StatusBadGateway},
			status: http.StatusBadGateway, logged: true,
			want: `{"success":false,"error":{"code":"BAD_GATEWAY","message":"upstream is down"}}`,
		},
		{
			name: "status outside the error range becomes 500", err: &apperror.AppError{Code: "CUSTOM", Message: "odd", HTTPStatus: http.StatusOK},
			status: http.StatusInternalServerError, logged: true,
			want: `{"success":false,"error":{"code":"CUSTOM","message":"odd"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			rec := httptest.NewRecorder()

			Error(rec, newRequest(t, &logs), tt.err)

			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.JSONEq(t, tt.want, rec.Body.String())
			if tt.leakage != "" {
				assert.NotContains(t, rec.Body.String(), tt.leakage)
			}
			if !tt.logged {
				assert.Empty(t, logs.String(), "client errors are not logged")
			}
		})
	}
}

func TestError_LogsServerErrorsWithCauseAndRequestID(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	rec := httptest.NewRecorder()

	Error(rec, newRequest(t, &logs), apperror.Internal("list tasks", errors.New("deadlock found")))

	var entry map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &entry), "log: %s", logs.String())
	assert.Equal(t, "ERROR", entry["level"])
	assert.Equal(t, "request failed", entry["msg"])
	assert.Equal(t, "req-1", entry["request_id"])
	assert.InDelta(t, http.StatusInternalServerError, entry["status"], 0)
	assert.Equal(t, apperror.CodeInternal, entry["code"])
	assert.Equal(t, "INTERNAL_ERROR: list tasks: deadlock found", entry["error"])
}

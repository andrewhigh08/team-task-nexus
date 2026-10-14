package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/response"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

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

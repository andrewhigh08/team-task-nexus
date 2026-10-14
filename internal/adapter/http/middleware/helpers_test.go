package middleware_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// newTestLogger returns a debug-level JSON logger writing to buf.
func newTestLogger(buf *bytes.Buffer) *logger.Logger {
	return logger.New(logger.Config{Level: "debug", Format: "json", Output: buf})
}

// logRecords returns the JSON log records written to buf.
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

// serve runs req through h and returns the recorded response.
func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// noContent answers 204 without a body.
func noContent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

package middleware_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
)

// panicking is a handler that panics with value; its frame must show up in the logged stack.
func panicking(value any) http.HandlerFunc {
	return func(http.ResponseWriter, *http.Request) { panic(value) }
}

func TestRecoverer_AnswersAPanicWith500(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value any
	}{
		{name: "string", value: "boom"},
		{name: "error", value: errors.New("kaboom")},
		{name: "integer", value: 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			log := newTestLogger(&logs)
			// The middleware between Recoverer and the panic must not hide where it happened.
			h := middleware.RequestID(middleware.Recoverer(log)(middleware.AccessLog(log)(middleware.Metrics(panicking(tt.value)))))
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/tasks/7", http.NoBody)
			req.Header.Set(middleware.RequestIDHeader, "req-panic")

			rec := serve(h, req)

			assert.Equal(t, http.StatusInternalServerError, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.Equal(t, "req-panic", rec.Header().Get(middleware.RequestIDHeader))
			assert.JSONEq(t, `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`, rec.Body.String())
			records := logRecords(t, &logs)
			require.Len(t, records, 2, "the access log and the panic")
			entry := records[1]
			assert.Equal(t, "ERROR", entry["level"])
			assert.Equal(t, "panic recovered", entry["msg"])
			assert.Equal(t, "req-panic", entry["request_id"])
			assert.Equal(t, http.MethodPatch, entry["method"])
			assert.Equal(t, "/api/v1/tasks/7", entry["path"])
			assert.Equal(t, fmt.Sprint(tt.value), entry["panic"])
			assert.Contains(t, entry["stack"], "recoverer_test.go", "the stack leads to the panic")
		})
	}
}

func TestRecoverer_KeepsAStartedResponse(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	h := middleware.Recoverer(newTestLogger(&logs))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("too late")
	}))

	rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "partial", rec.Body.String(), "no second response is written")
	records := logRecords(t, &logs)
	require.Len(t, records, 1)
	assert.Equal(t, "panic recovered", records[0]["msg"])
}

func TestRecoverer_PassesOnAnAbortedHandler(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	h := middleware.Recoverer(newTestLogger(&logs))(panicking(http.ErrAbortHandler))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() { serve(h, req) }, "net/http aborts the response")
	assert.Empty(t, logs.String())
}

func TestRecoverer_LeavesAServedRequestAlone(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	h := middleware.Recoverer(newTestLogger(&logs))(http.HandlerFunc(noContent))

	rec := serve(h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Empty(t, logs.String())
}

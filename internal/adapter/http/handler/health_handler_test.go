package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// pingerFunc turns a function into a port.Pinger.
type pingerFunc func(ctx context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

func answering(err error) port.Pinger {
	return pingerFunc(func(context.Context) error { return err })
}

// probe serves a request to h through a context that logs to logs.
func probe(t *testing.T, h http.HandlerFunc, logs *bytes.Buffer) *httptest.ResponseRecorder {
	t.Helper()
	ctx := logger.NewContext(t.Context(), logger.New(logger.Config{Format: "json", Output: logs}))
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/health", http.NoBody))
	return rec
}

func TestHealthHandler_Live(t *testing.T) {
	t.Parallel()
	h := NewHealthHandler(map[string]port.Pinger{"mysql": answering(errors.New("down"))})

	rec := probe(t, h.Live, &bytes.Buffer{})

	assert.Equal(t, http.StatusOK, rec.Code, "liveness does not depend on the dependencies")
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestHealthHandler_Ready(t *testing.T) {
	t.Parallel()
	errDial := errors.New("dial tcp 10.0.0.5:3306: connection refused")
	tests := []struct {
		name       string
		checks     map[string]port.Pinger
		wantStatus int
		wantBody   string
		wantLogged []string // checks whose failure is logged
	}{
		{
			name:       "every dependency answers",
			checks:     map[string]port.Pinger{"mysql": answering(nil), "redis": answering(nil)},
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ready","checks":{"mysql":"ok","redis":"ok"}}`,
		},
		{
			name:       "mysql is down",
			checks:     map[string]port.Pinger{"mysql": answering(errDial), "redis": answering(nil)},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"not_ready","checks":{"mysql":"unavailable","redis":"ok"}}`,
			wantLogged: []string{"mysql"},
		},
		{
			name:       "both are down",
			checks:     map[string]port.Pinger{"mysql": answering(errDial), "redis": answering(errDial)},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"not_ready","checks":{"mysql":"unavailable","redis":"unavailable"}}`,
			wantLogged: []string{"mysql", "redis"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer

			rec := probe(t, NewHealthHandler(tt.checks).Ready, &logs)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
			assert.NotContains(t, rec.Body.String(), "10.0.0.5", "the cause stays out of the public probe")
			var logged []string
			for line := range strings.Lines(logs.String()) {
				var entry map[string]any
				require.NoError(t, json.Unmarshal([]byte(line), &entry))
				assert.Equal(t, "readiness check failed", entry["msg"])
				assert.Equal(t, errDial.Error(), entry["error"])
				check, _ := entry["check"].(string)
				logged = append(logged, check)
			}
			assert.ElementsMatch(t, tt.wantLogged, logged)
		})
	}
}

func TestHealthHandler_Ready_BoundsEveryCheck(t *testing.T) {
	t.Parallel()
	var deadline time.Time
	h := NewHealthHandler(map[string]port.Pinger{"mysql": pingerFunc(func(ctx context.Context) error {
		deadline, _ = ctx.Deadline()
		return nil
	})})
	start := time.Now()

	rec := probe(t, h.Ready, &bytes.Buffer{})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.WithinDuration(t, start.Add(pingTimeout), deadline, time.Second, "a check runs within pingTimeout")
}

func TestHealthHandler_Ready_ChecksConcurrently(t *testing.T) {
	t.Parallel()
	// Each check waits for the other to start, which only happens when they run concurrently;
	// run one after the other, the first would wait until its timeout and fail.
	var started atomic.Int32
	bothStarted := make(chan struct{})
	waitForTheOther := pingerFunc(func(ctx context.Context) error {
		if started.Add(1) == 2 {
			close(bothStarted)
		}
		select {
		case <-bothStarted:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	h := NewHealthHandler(map[string]port.Pinger{"mysql": waitForTheOther, "redis": waitForTheOther})

	rec := probe(t, h.Ready, &bytes.Buffer{})

	assert.Equal(t, http.StatusOK, rec.Code)
}

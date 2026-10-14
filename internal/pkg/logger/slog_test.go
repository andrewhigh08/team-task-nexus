package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func logRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec), "log line: %s", buf.String())
	return rec
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	assert.Equal(t, "info", cfg.Level)
	assert.Equal(t, "json", cfg.Format)
	assert.True(t, cfg.AddSource)
	assert.Equal(t, time.RFC3339Nano, cfg.TimeFormat)
	assert.Same(t, os.Stdout, cfg.Output)
}

func TestNew_TimeFieldIsRFC3339NanoUTC(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := New(Config{Level: "info", Format: "json", Output: &buf})

	log.Info("hello")

	ts, ok := logRecord(t, &buf)["time"].(string)
	require.True(t, ok, "time must be a string")
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(ts, "Z"), "time must be rendered in UTC, got %q", ts)
	assert.WithinDuration(t, time.Now(), parsed, time.Minute)
}

func TestNew_CustomTimeFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := New(Config{Format: "json", TimeFormat: time.DateOnly, Output: &buf})

	log.Info("hello")

	ts, ok := logRecord(t, &buf)["time"].(string)
	require.True(t, ok)
	_, err := time.Parse(time.DateOnly, ts)
	assert.NoError(t, err)
}

func TestNew_LevelThreshold(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		level     string
		threshold slog.Level
	}{
		{name: "debug", level: "debug", threshold: slog.LevelDebug},
		{name: "info", level: "info", threshold: slog.LevelInfo},
		{name: "warn", level: "warn", threshold: slog.LevelWarn},
		{name: "error", level: "error", threshold: slog.LevelError},
		{name: "empty defaults to info", level: "", threshold: slog.LevelInfo},
		{name: "unknown defaults to info", level: "verbose", threshold: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			log := New(Config{Level: tt.level, Output: &bytes.Buffer{}})

			for _, lvl := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
				assert.Equal(t, lvl >= tt.threshold, log.Enabled(t.Context(), lvl), "level %s", lvl)
			}
		})
	}
}

func TestNew_TextFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := New(Config{Format: "text", Output: &buf})

	log.Info("hello", slog.String("key", "value"))

	line := buf.String()
	assert.Regexp(t, `^time=\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z `, line)
	assert.Contains(t, line, "level=INFO")
	assert.Contains(t, line, "msg=hello")
	assert.Contains(t, line, "key=value")
}

func TestNew_UnknownFormatFallsBackToJSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := New(Config{Format: "yaml", Output: &buf})

	log.Info("hello")

	rec := logRecord(t, &buf)
	assert.Equal(t, "hello", rec["msg"])
	assert.Equal(t, "INFO", rec["level"])
}

func TestNew_AddSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		addSource bool
	}{
		{name: "enabled", addSource: true},
		{name: "disabled", addSource: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			log := New(Config{Format: "json", AddSource: tt.addSource, Output: &buf})

			log.Info("hello")

			rec := logRecord(t, &buf)
			if !tt.addSource {
				assert.NotContains(t, rec, slog.SourceKey)
				return
			}
			source, ok := rec[slog.SourceKey].(map[string]any)
			require.True(t, ok, "source must be an object")
			file, _ := source["file"].(string)
			assert.True(t, strings.HasSuffix(file, "slog_test.go"), "source file %q must be the caller", file)
		})
	}
}

func TestFromContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		requestID string
		userID    int64
		want      map[string]any
	}{
		{name: "request and user id", requestID: "req-1", userID: 42, want: map[string]any{"request_id": "req-1", "user_id": float64(42)}},
		{name: "request id only", requestID: "req-7", want: map[string]any{"request_id": "req-7"}},
		{name: "user id only", userID: 9, want: map[string]any{"user_id": float64(9)}},
		{name: "neither", want: map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			ctx := NewContext(t.Context(), New(Config{Format: "json", Output: &buf}))
			if tt.requestID != "" {
				ctx = WithRequestIDContext(ctx, tt.requestID)
			}
			if tt.userID != 0 {
				ctx = WithUserIDContext(ctx, tt.userID)
			}

			FromContext(ctx).Info("hello")

			rec := logRecord(t, &buf)
			for _, key := range []string{"request_id", "user_id"} {
				want, present := tt.want[key]
				if !present {
					assert.NotContains(t, rec, key)
					continue
				}
				assert.Equal(t, want, rec[key])
			}
		})
	}
}

func TestFromContext_ReturnsTheStoredLoggerWhenThereIsNothingToAdd(t *testing.T) {
	t.Parallel()
	log := New(Config{Output: &bytes.Buffer{}})

	assert.Same(t, log, FromContext(NewContext(t.Context(), log)))
}

func TestFromContext_FallsBackToTheDefaultLogger(t *testing.T) {
	t.Parallel()

	got := FromContext(t.Context())

	require.NotNil(t, got)
	assert.Same(t, slog.Default(), got.Logger)
}

func TestFromContext_KeepsComponent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := New(Config{Format: "json", Output: &buf}).WithComponent("http")
	ctx := WithRequestIDContext(NewContext(t.Context(), log), "req-1")

	FromContext(ctx).Info("hello")

	rec := logRecord(t, &buf)
	assert.Equal(t, "http", rec["component"])
	assert.Equal(t, "req-1", rec["request_id"])
}

func TestContextHelpers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	assert.Empty(t, RequestIDFromContext(ctx))
	assert.Zero(t, UserIDFromContext(ctx))

	ctx = WithUserIDContext(WithRequestIDContext(ctx, "abc"), 7)
	assert.Equal(t, "abc", RequestIDFromContext(ctx))
	assert.Equal(t, int64(7), UserIDFromContext(ctx))
}

func TestContextHelpers_IgnoreForeignKeys(t *testing.T) {
	t.Parallel()
	type foreignKey string
	ctx := context.WithValue(t.Context(), foreignKey("request_id"), "abc")
	ctx = context.WithValue(ctx, foreignKey("user_id"), int64(7))
	ctx = context.WithValue(ctx, foreignKey("logger"), New(Config{Output: &bytes.Buffer{}}))

	assert.Empty(t, RequestIDFromContext(ctx), "keys of another type must not collide with the package keys")
	assert.Zero(t, UserIDFromContext(ctx))
	assert.Same(t, slog.Default(), FromContext(ctx).Logger)
}

func TestWithComponent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := New(Config{Format: "json", Output: &buf})

	log.WithComponent("app").Info("hello")

	assert.Equal(t, "app", logRecord(t, &buf)["component"])
}

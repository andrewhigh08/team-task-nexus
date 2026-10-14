package logger

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sourceFunction returns the function a JSON log record is attributed to.
func sourceFunction(t *testing.T, rec map[string]any) string {
	t.Helper()
	source, ok := rec[slog.SourceKey].(map[string]any)
	require.True(t, ok, "source must be an object")
	function, _ := source["function"].(string)
	return function
}

// forwardPrintf calls l.Printf the way go-redis does, through one forwarding call.
func forwardPrintf(ctx context.Context, l *RedisLogger, format string, v ...any) {
	l.Printf(ctx, format, v...)
}

func TestRedisLogger_Printf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "formats like fmt.Sprintf",
			format: "redis: connection pool: failed to dial after %d attempts: %v",
			args:   []any{3, errors.New("connection refused")},
			want:   "redis: connection pool: failed to dial after 3 attempts: connection refused",
		},
		{name: "message without verbs", format: "redis: discarding bad connection", want: "redis: discarding bad connection"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			l := NewRedisLogger(New(Config{Format: "json", AddSource: true, Output: &buf}).WithComponent("redis"))

			forwardPrintf(t.Context(), l, tt.format, tt.args...)

			rec := logRecord(t, &buf)
			assert.Equal(t, "WARN", rec["level"])
			assert.Equal(t, tt.want, rec["msg"])
			assert.Equal(t, "redis", rec["component"])
			assert.Contains(t, sourceFunction(t, rec), "TestRedisLogger_Printf", "the source is the caller of the forwarding call")
		})
	}
}

func TestMySQLLogger_Print(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []any
		want string
	}{
		{name: "position prefix and error", args: []any{"packets.go:58 ", io.ErrUnexpectedEOF}, want: "packets.go:58 unexpected EOF"},
		{name: "operands that are not strings are spaced", args: []any{errors.New("busy buffer"), 2}, want: "busy buffer 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			l := NewMySQLLogger(New(Config{Format: "json", AddSource: true, Output: &buf}).WithComponent("mysql"))

			l.Print(tt.args...)

			rec := logRecord(t, &buf)
			assert.Equal(t, "WARN", rec["level"])
			assert.Equal(t, tt.want, rec["msg"])
			assert.Equal(t, "mysql", rec["component"])
			assert.Contains(t, sourceFunction(t, rec), "TestMySQLLogger_Print", "the source is the caller of Print")
		})
	}
}

func TestDriverLoggers_SkipMessagesBelowTheLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := New(Config{Level: "error", Output: &buf})

	NewRedisLogger(log).Printf(t.Context(), "redis: %s", "connection pool exhausted")
	NewMySQLLogger(log).Print("closing bad idle connection: ", io.EOF)

	assert.Empty(t, buf.String())
}

// TestRedisLogger_ReceivesTheMessagesOfGoRedis installs the adapter as the process-wide logger
// of go-redis, which no other test of this package uses.
func TestRedisLogger_ReceivesTheMessagesOfGoRedis(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	redis.SetLogger(NewRedisLogger(New(Config{Format: "json", AddSource: true, Output: &buf}).WithComponent("redis")))
	errDial := errors.New("dial refused by the test")
	rdb := redis.NewClient(&redis.Options{
		Addr:          "redis.test:6379",
		Dialer:        func(context.Context, string, string) (net.Conn, error) { return nil, errDial },
		DialerRetries: 1,
		MaxRetries:    -1,
	})

	err := rdb.Ping(t.Context()).Err()
	require.NoError(t, rdb.Close())

	require.ErrorIs(t, err, errDial)
	rec := logRecord(t, &buf)
	assert.Equal(t, "WARN", rec["level"])
	assert.Equal(t, "redis", rec["component"])
	assert.Contains(t, rec["msg"], errDial.Error())
	function := sourceFunction(t, rec)
	assert.True(t, strings.HasPrefix(function, "github.com/redis/go-redis/v9/"), "source %q must be in go-redis", function)
	assert.NotContains(t, function, "Printf", "the source is the code that logged, not a forwarding call")
}

func TestMySQLLogger_ReceivesTheMessagesOfTheDriver(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	cfg := mysql.NewConfig()
	cfg.Addr = "mysql.test:3306"
	cfg.Logger = NewMySQLLogger(New(Config{Format: "json", AddSource: true, Output: &buf}).WithComponent("mysql"))
	// The server end is closed at once, so the handshake fails with a read error the driver logs.
	cfg.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		return client, server.Close()
	}
	connector, err := mysql.NewConnector(cfg)
	require.NoError(t, err)
	db := sql.OpenDB(connector)

	require.Error(t, db.PingContext(t.Context()))
	require.NoError(t, db.Close())

	rec := logRecord(t, &buf)
	assert.Equal(t, "WARN", rec["level"])
	assert.Equal(t, "mysql", rec["component"])
	assert.NotEmpty(t, rec["msg"])
	function := sourceFunction(t, rec)
	assert.True(t, strings.HasPrefix(function, "github.com/go-sql-driver/mysql."), "source %q must be in the driver", function)
}

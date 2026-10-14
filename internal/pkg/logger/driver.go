package logger

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"
)

// RedisLogger writes the messages of go-redis to a Logger. go-redis logs through one logger for
// the whole process, plain text on stderr by default, so install a RedisLogger once at startup
// with redis.SetLogger.
type RedisLogger struct {
	log *Logger
}

// NewRedisLogger returns a RedisLogger writing to l.
func NewRedisLogger(l *Logger) *RedisLogger {
	return &RedisLogger{log: l}
}

// Printf writes the message, formatted as fmt.Sprintf does, at warn level: go-redis logs
// failures, such as a connection it could not dial. The record is attributed to the go-redis
// code that logged it, which reaches Printf through one forwarding call of go-redis itself.
func (r *RedisLogger) Printf(ctx context.Context, format string, v ...any) {
	r.log.warnFromCaller(ctx, 2, fmt.Sprintf(format, v...))
}

// MySQLLogger writes the messages of go-sql-driver/mysql to a Logger. Set it as the Logger of
// the mysql.Config a connector is built from; without it the driver writes plain text to
// stderr, and mysql.SetLogger would change the logger of every connection in the process.
type MySQLLogger struct {
	log *Logger
}

// NewMySQLLogger returns a MySQLLogger writing to l.
func NewMySQLLogger(l *Logger) *MySQLLogger {
	return &MySQLLogger{log: l}
}

// Print writes its operands, formatted as fmt.Sprint does, at warn level: the driver logs
// failures, such as a broken connection it drops. The record is attributed to the driver code
// that called Print.
func (m *MySQLLogger) Print(v ...any) {
	m.log.warnFromCaller(context.Background(), 1, fmt.Sprint(v...))
}

// warnFromCaller writes msg at warn level and attributes it to the function depth calls above
// the caller of warnFromCaller, so that the source of the record is the code that logged rather
// than an adapter.
func (l *Logger) warnFromCaller(ctx context.Context, depth int, msg string) {
	if !l.Enabled(ctx, slog.LevelWarn) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(depth+2, pcs[:]) // skip runtime.Callers and warnFromCaller
	_ = l.Handler().Handle(ctx, slog.NewRecord(time.Now(), slog.LevelWarn, msg, pcs[0]))
}

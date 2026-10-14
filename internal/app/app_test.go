package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/andrewhigh08/team-task-nexus/internal/config"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// syncBuffer collects log output written from several goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// records returns the JSON log records written so far.
func (b *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var records []map[string]any
	for line := range strings.Lines(b.buf.String()) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "log line: %s", line)
		records = append(records, rec)
	}
	return records
}

func newTestLogger(out *syncBuffer) *logger.Logger {
	return logger.New(logger.Config{Level: "debug", Format: "json", Output: out})
}

// closedPort returns a local TCP port nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

// testConfig returns a valid configuration whose MySQL and Redis addresses refuse connections.
func testConfig(t *testing.T, connectTimeout time.Duration) *config.Config {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: closedPort(t), ReadHeaderTimeout: time.Second, ReadTimeout: time.Second,
			WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second,
		},
		Log: config.LogConfig{Level: "debug", Format: "json"},
		Database: config.DatabaseConfig{
			Host: "127.0.0.1", Port: closedPort(t), User: "app", Name: "team_task_nexus",
			MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnectTimeout: connectTimeout,
		},
		Redis:     config.RedisConfig{Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(closedPort(t)))},
		JWT:       config.JWTConfig{Secret: strings.Repeat("k", 32), Issuer: "team-task-nexus", TTL: time.Hour},
		Password:  config.PasswordConfig{BcryptCost: bcrypt.MinCost},
		RateLimit: config.RateLimitConfig{APIPerMinute: 100},
	}
	require.NoError(t, cfg.Validate())
	return cfg
}

func TestNew_RequiresConfiguration(t *testing.T) {
	t.Parallel()

	got, err := New(t.Context(), nil, nil, "test")

	require.Error(t, err)
	assert.Nil(t, got)
}

func TestNew_FailsWhenADependencyIsUnreachable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		cancel     bool
		budget     time.Duration
		wantErr    error
		maxElapsed time.Duration
	}{
		{name: "connect timeout elapses", budget: 300 * time.Millisecond, maxElapsed: 5 * time.Second},
		{name: "context canceled", cancel: true, budget: time.Minute, wantErr: context.Canceled, maxElapsed: 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			var logs syncBuffer

			start := time.Now()
			got, err := New(ctx, testConfig(t, tt.budget), newTestLogger(&logs), "test")

			assert.Less(t, time.Since(start), tt.maxElapsed)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), "mysql not reachable after")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			records := logs.records(t)
			require.NotEmpty(t, records, "every failed attempt is logged")
			assert.Equal(t, "waiting for dependency", records[0]["msg"])
			assert.Equal(t, "mysql", records[0]["dependency"])
			assert.Equal(t, "app", records[0]["component"])
		})
	}
}

func TestOpenRedis_FailsWhenRedisIsUnreachable(t *testing.T) {
	t.Parallel()
	var logs syncBuffer
	log := newTestLogger(&logs)
	a := &App{cfg: testConfig(t, 300*time.Millisecond), base: log, log: log}

	err := a.openRedis(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis not reachable after")
	require.NotNil(t, a.rdb, "the client is kept for Close to release")
	require.NoError(t, a.Close())
}

func TestWaitFor(t *testing.T) {
	t.Parallel()
	errDown := errors.New("connection refused")
	tests := []struct {
		name         string
		failures     int
		budget       time.Duration
		wantAttempts int
		wantErr      bool
	}{
		{name: "reachable at once", failures: 0, budget: time.Second, wantAttempts: 1},
		{name: "reachable on the third attempt", failures: 2, budget: 5 * time.Second, wantAttempts: 3},
		// Attempts at 0 and 250ms; the next one would come at 750ms, after the budget is spent.
		{name: "never reachable", failures: 100, budget: 600 * time.Millisecond, wantAttempts: 2, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs syncBuffer
			cfg := &config.Config{Database: config.DatabaseConfig{ConnectTimeout: tt.budget}}
			a := &App{cfg: cfg, log: newTestLogger(&logs)}
			attempts := 0
			ping := func(ctx context.Context) error {
				attempts++
				deadline, ok := ctx.Deadline()
				require.True(t, ok, "every attempt is bounded by the connect timeout")
				assert.WithinDuration(t, time.Now().Add(tt.budget), deadline, tt.budget)
				if attempts <= tt.failures {
					return errDown
				}
				return nil
			}

			err := a.waitFor(t.Context(), "mysql", ping)

			assert.Equal(t, tt.wantAttempts, attempts)
			assert.Len(t, logs.records(t), min(attempts, tt.failures), "one warning per failed attempt")
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errDown, "the last failure is reported")
			assert.Contains(t, err.Error(), "mysql not reachable after 2 attempts")
		})
	}
}

// newServedApp returns an App serving handler on a free loopback port, without databases.
func newServedApp(t *testing.T, handler http.Handler) (*App, string) {
	t.Helper()
	var logs syncBuffer
	log := newTestLogger(&logs)
	a := &App{cfg: testConfig(t, time.Second), base: log, log: log, router: handler}
	a.srv = a.newServer()
	a.srv.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(a.cfg.Server.Port))
	return a, "http://" + a.srv.Addr
}

func TestNewServer_UsesTheConfiguredTimeouts(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t, time.Second)
	cfg.Server.Port, cfg.Server.ReadHeaderTimeout, cfg.Server.ReadTimeout = 9090, 2*time.Second, 3*time.Second
	cfg.Server.WriteTimeout, cfg.Server.IdleTimeout = 4*time.Second, 5*time.Second
	log := newTestLogger(&syncBuffer{})
	handler := http.NewServeMux()
	a := &App{cfg: cfg, base: log, log: log, router: handler}

	srv := a.newServer()

	assert.Equal(t, ":9090", srv.Addr)
	assert.Equal(t, 2*time.Second, srv.ReadHeaderTimeout)
	assert.Equal(t, 3*time.Second, srv.ReadTimeout)
	assert.Equal(t, 4*time.Second, srv.WriteTimeout)
	assert.Equal(t, 5*time.Second, srv.IdleTimeout)
	assert.NotNil(t, srv.ErrorLog)
	assert.Same(t, handler, srv.Handler)
	assert.Same(t, handler, a.Handler())
}

func TestRun_ServesUntilTheContextIsCanceled(t *testing.T) {
	t.Parallel()
	a, baseURL := newServedApp(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL, http.NoBody)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusTeapot
	}, 5*time.Second, 20*time.Millisecond, "the server answers while running")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was canceled")
	}
	require.NoError(t, a.Close(), "Run has already released the resources; Close stays safe")
}

func TestRun_FailsWhenThePortIsTaken(t *testing.T) {
	t.Parallel()
	a, _ := newServedApp(t, http.NotFoundHandler())
	taken, err := net.Listen("tcp", a.srv.Addr)
	require.NoError(t, err)
	defer taken.Close()

	err = a.Run(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "http server")
}

func TestClose_IsIdempotent(t *testing.T) {
	t.Parallel()
	a := &App{}

	require.NoError(t, a.Close())
	require.NoError(t, a.Close())
}

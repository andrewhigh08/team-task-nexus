package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/andrewhigh08/team-task-nexus/internal/config"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/service"
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

// idleRedis returns a Redis client for a local port nothing listens on. The client connects only
// when a command is sent, so it can stand in for a real one where only Close matters.
func idleRedis(t *testing.T) *redis.Client {
	t.Helper()
	return redis.NewClient(&redis.Options{Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(closedPort(t)))})
}

// requireRedisClosed fails the test unless rdb has been closed.
func requireRedisClosed(t *testing.T, rdb *redis.Client) {
	t.Helper()
	require.ErrorIs(t, rdb.Ping(t.Context()).Err(), redis.ErrClosed, "the Redis client is released")
}

// hangUpPort returns a local TCP port whose server closes every connection it accepts, so that a
// MySQL client fails its handshake with a read error the driver logs.
func hangUpPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, l.Close()) })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
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
		RateLimit: config.RateLimitConfig{APIPerMinute: 100, AuthPerMinute: 10},
		Cache:     config.CacheConfig{TaskListTTL: time.Minute},
		Outbox:    config.OutboxConfig{PollInterval: time.Second, BatchSize: 10, MaxAttempts: 3, Lease: 30 * time.Second},
		Notify:    config.NotifyConfig{WebhookTimeout: 5 * time.Second},
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

func TestMySQLPools_LogDriverMessagesWithTheMySQLComponent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		open func(ctx context.Context, a *App) error
	}{
		{name: "application pool", open: func(ctx context.Context, a *App) error { return a.openMySQL(ctx) }},
		{name: "migration pool", open: func(ctx context.Context, a *App) error {
			connector, err := a.mysqlConnector(a.cfg.MigrationDSN())
			if err != nil {
				return err
			}
			return migrateUp(ctx, connector)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs syncBuffer
			log := newTestLogger(&logs)
			cfg := testConfig(t, 300*time.Millisecond)
			cfg.Database.Port = hangUpPort(t)
			a := &App{cfg: cfg, base: log, log: log.WithComponent("app")}

			err := tt.open(t.Context(), a)

			require.Error(t, err)
			require.NoError(t, a.Close())
			driverRecords := 0
			for _, rec := range logs.records(t) {
				if rec["component"] == "mysql" {
					driverRecords++
					assert.Equal(t, "WARN", rec["level"])
				}
			}
			assert.Positive(t, driverRecords, "the driver reports the broken connections through the application log")
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

// fakeWorker runs until its context is canceled, calls onStop if set, and returns err.
type fakeWorker struct {
	err     error
	onStop  func()
	started chan struct{}
	stopped chan struct{}
}

func newFakeWorker(err error) *fakeWorker {
	return &fakeWorker{err: err, started: make(chan struct{}), stopped: make(chan struct{})}
}

func (w *fakeWorker) Run(ctx context.Context) error {
	close(w.started)
	<-ctx.Done()
	if w.onStop != nil {
		w.onStop()
	}
	close(w.stopped)
	return w.err
}

// newServedApp returns an App serving handler on a free loopback port, with a fake worker, an
// idle Redis client for Run to release and no MySQL pool.
func newServedApp(t *testing.T, handler http.Handler, workerErr error) (*App, string, *fakeWorker) {
	t.Helper()
	var logs syncBuffer
	log := newTestLogger(&logs)
	a := &App{cfg: testConfig(t, time.Second), base: log, log: log, rdb: idleRedis(t), router: handler}
	a.srv = a.newServer()
	a.srv.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(a.cfg.Server.Port))
	worker := newFakeWorker(workerErr)
	a.dispatcher = worker
	return a, "http://" + a.srv.Addr, worker
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
	a, baseURL, worker := newServedApp(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}), nil)
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

	awaitWhileRunning(t, worker.started, done, "the workers started")
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was canceled")
	}
	assert.True(t, isClosed(worker.stopped), "Run waits for the workers")
	requireRedisClosed(t, a.rdb)
	require.NoError(t, a.Close(), "Run has already released the resources; Close stays safe")
}

// TestRun_StopsTheWorkersAfterTheServer shuts the app down while a request is in flight. The
// workers must keep running until the server has finished it, so the events the request
// commits can still be delivered.
func TestRun_StopsTheWorkersAfterTheServer(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var answered atomic.Bool
	a, baseURL, worker := newServedApp(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		answered.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}), nil)
	var answeredBeforeStop bool
	worker.onStop = func() { answeredBeforeStop = answered.Load() }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	awaitWhileRunning(t, worker.started, done, "the workers started")
	go func() {
		// Retried until the server listens, or until the test ends; only the first request
		// reaches the handler.
		for {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL, http.NoBody)
			if !assert.NoError(t, err) {
				return
			}
			if resp, err := http.DefaultClient.Do(req); err == nil {
				_ = resp.Body.Close()
				return
			}
			select {
			case <-t.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()

	awaitWhileRunning(t, entered, done, "the request reached the handler")
	cancel()
	// A worker stopped together with the server would see the request unanswered.
	time.AfterFunc(100*time.Millisecond, func() { close(release) })

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was canceled")
	}
	assert.True(t, answeredBeforeStop, "the workers are stopped only after the server has drained")
}

func TestRun_ReportsAFailedWorker(t *testing.T) {
	t.Parallel()
	errWorker := errors.New("outbox unreachable")
	a, _, worker := newServedApp(t, http.NotFoundHandler(), errWorker)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-worker.started
		cancel()
	}()

	err := a.Run(ctx)

	require.ErrorIs(t, err, errWorker)
	assert.Contains(t, err.Error(), "workers: outbox unreachable")
}

func TestRun_FailsWhenThePortIsTaken(t *testing.T) {
	t.Parallel()
	a, _, worker := newServedApp(t, http.NotFoundHandler(), nil)
	taken, err := net.Listen("tcp", a.srv.Addr)
	require.NoError(t, err)
	defer taken.Close()

	err = a.Run(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "http server")
	assert.True(t, isClosed(worker.stopped), "the workers are stopped and waited for")
	requireRedisClosed(t, a.rdb)
}

// awaitWhileRunning waits until ch is closed while Run, which reports to done, keeps running. It
// fails the test if Run returns first or nothing happens within five seconds.
func awaitWhileRunning(t *testing.T, ch <-chan struct{}, done <-chan error, what string) {
	t.Helper()
	select {
	case <-ch:
	case err := <-done:
		t.Fatalf("Run returned before %s: %v", what, err)
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting until %s", what)
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// newSenderApp returns an App configured to deliver events as notifyCfg says, logging to logs.
func newSenderApp(t *testing.T, notifyCfg config.NotifyConfig, logs *syncBuffer) *App {
	t.Helper()
	cfg := testConfig(t, time.Second)
	cfg.Notify = notifyCfg
	log := newTestLogger(logs)
	return &App{cfg: cfg, base: log, log: log}
}

func testOutboxEvent() domain.OutboxEvent {
	return domain.OutboxEvent{ID: 42, Type: domain.EventTaskAssigned, Payload: []byte(`{"task_id": 11}`), Attempts: 1}
}

func TestNewSender_FollowsTheConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("log without a webhook", func(t *testing.T) {
		t.Parallel()
		var logs syncBuffer
		a := newSenderApp(t, config.NotifyConfig{WebhookTimeout: time.Second}, &logs)

		require.NoError(t, a.newSender().Send(t.Context(), testOutboxEvent()))

		records := logs.records(t)
		require.Len(t, records, 1)
		assert.Equal(t, "event delivered", records[0]["msg"])
		assert.Equal(t, "notify", records[0]["component"])
	})

	t.Run("webhook at the configured URL", func(t *testing.T) {
		t.Parallel()
		posted := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			posted <- r.Method + " " + r.URL.Path
		}))
		t.Cleanup(srv.Close)
		var logs syncBuffer
		a := newSenderApp(t, config.NotifyConfig{WebhookURL: srv.URL + "/events", WebhookTimeout: time.Second}, &logs)

		require.NoError(t, a.newSender().Send(t.Context(), testOutboxEvent()))

		assert.Equal(t, "POST /events", <-posted)
		assert.Empty(t, logs.records(t), "the event goes to the webhook only")
	})

	t.Run("webhook within the configured timeout", func(t *testing.T) {
		t.Parallel()
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			<-release // answers only once the test is over
		}))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) }) // runs first, so Close does not wait for the handler
		a := newSenderApp(t, config.NotifyConfig{WebhookURL: srv.URL, WebhookTimeout: 50 * time.Millisecond}, &syncBuffer{})
		// A sender without the configured timeout would wait for this deadline instead.
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		start := time.Now()
		err := a.newSender().Send(ctx, testOutboxEvent())

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(start), 5*time.Second)
	})
}

func TestDispatcherConfig_FollowsTheConfiguration(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t, time.Second)
	cfg.Outbox = config.OutboxConfig{PollInterval: 250 * time.Millisecond, BatchSize: 20, MaxAttempts: 7, Lease: time.Minute}
	cfg.Notify.WebhookTimeout = 15 * time.Second
	a := &App{cfg: cfg}

	assert.Equal(t, service.DispatcherConfig{
		PollInterval: 250 * time.Millisecond,
		BatchSize:    20,
		MaxAttempts:  7,
		Lease:        time.Minute,
		SendTimeout:  15 * time.Second,
	}, a.dispatcherConfig())
}

func TestClose_ReleasesTheResourcesOnce(t *testing.T) {
	t.Parallel()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	mock.ExpectClose()
	a := &App{db: sqlx.NewDb(sqlDB, "mysql"), rdb: idleRedis(t)}

	require.NoError(t, a.Close())
	require.NoError(t, a.Close(), "the second Close does not close the Redis client again")

	requireRedisClosed(t, a.rdb)
	assert.NoError(t, mock.ExpectationsWereMet(), "the MySQL pool is closed")
}

// Package integration holds the end-to-end tests of the service. They boot the application
// against MySQL and Redis containers started with testcontainers and talk to it over HTTP, so
// Docker is required; under -short every test is skipped.
package integration

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/andrewhigh08/team-task-nexus/internal/app"
	"github.com/andrewhigh08/team-task-nexus/internal/config"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// Values the suite boots the service with; the tests rely on them directly.
const (
	appVersion  = "integration-test"
	jwtSecret   = "integration-test-secret-of-32-bytes-at-least"
	tokenIssuer = "team-task-nexus"
	tokenTTL    = time.Hour
	// unlimited is a rate limit the suite never reaches, so that only the rate limit test,
	// which boots an application of its own, is ever throttled.
	unlimited = 1_000_000

	setupTimeout    = 3 * time.Minute
	teardownTimeout = 30 * time.Second
)

// suite is the environment every test of the package shares: the containers, the webhook the
// events are delivered to, the booted application with its outbox dispatcher running, and the
// HTTP server in front of it. TestMain builds it once; tests only read it.
type suite struct {
	containers  *testContainers
	webhook     *webhookRecorder
	cfg         *config.Config
	app         *app.App
	server      *httptest.Server
	stopWorkers context.CancelFunc
	workersDone chan error
}

// shared is assigned once by TestMain before any test runs and stays nil under -short.
var shared *suite

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration suite:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	s, err := startSuite()
	if err != nil {
		return 0, err
	}
	defer s.stop()
	shared = s
	return m.Run(), nil
}

// env returns the shared suite, skipping the test when there is none.
func env(t *testing.T) *suite {
	t.Helper()
	if shared == nil {
		t.Skip("integration tests need Docker and are skipped under -short")
	}
	return shared
}

func startSuite() (*suite, error) {
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	containers, err := startContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("start containers: %w", err)
	}
	s := &suite{containers: containers, webhook: newWebhookRecorder()}
	if err = s.boot(ctx); err != nil {
		s.stop()
		return nil, err
	}
	return s, nil
}

// boot starts the application the tests talk to, serving its handler and running its outbox
// dispatcher as Run would, without listening on a port of its own.
func (s *suite) boot(ctx context.Context) error {
	s.cfg = newConfig(s.containers, s.webhook.URL())
	if err := s.cfg.Validate(); err != nil {
		return fmt.Errorf("invalid test configuration: %w", err)
	}
	application, err := app.New(ctx, s.cfg, newLogger(), appVersion)
	if err != nil {
		return fmt.Errorf("boot application: %w", err)
	}
	s.app = application
	s.server = httptest.NewServer(application.Handler())

	workersCtx, stopWorkers := context.WithCancel(context.Background())
	s.stopWorkers = stopWorkers
	s.workersDone = make(chan error, 1)
	go func() { s.workersDone <- application.RunWorkers(workersCtx) }()
	return nil
}

// stop releases the suite in reverse order of creation; it tolerates a partially built one.
// The dispatcher is stopped before the application closes the connections it uses.
func (s *suite) stop() {
	if s.server != nil {
		s.server.Close()
	}
	if s.stopWorkers != nil {
		s.stopWorkers()
		if err := <-s.workersDone; err != nil {
			fmt.Fprintln(os.Stderr, "integration suite: workers:", err)
		}
	}
	if s.app != nil {
		if err := s.app.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "integration suite: close application:", err)
		}
	}
	s.webhook.Close()
	if err := s.containers.terminate(); err != nil {
		fmt.Fprintln(os.Stderr, "integration suite: terminate containers:", err)
	}
}

// newConfig builds the configuration of the application under test: what the environment of
// a deployment would produce, with rate limits the suite never reaches, the cheapest bcrypt
// cost and an outbox polled often enough for the tests to see deliveries within moments.
func newConfig(c *testContainers, webhookURL string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			// Never listened on: the tests serve the handler through httptest.
			Port:              8080,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       time.Minute,
			ShutdownTimeout:   5 * time.Second,
		},
		Log: config.LogConfig{Level: "info", Format: "json"},
		Database: config.DatabaseConfig{
			Host:            c.mysqlHost,
			Port:            c.mysqlPort,
			User:            mysqlUser,
			Password:        mysqlPassword,
			Name:            mysqlDatabase,
			MaxOpenConns:    25,
			MaxIdleConns:    10,
			ConnMaxLifetime: 5 * time.Minute,
			ConnectTimeout:  30 * time.Second,
		},
		Redis:     config.RedisConfig{Addr: c.redisAddr},
		JWT:       config.JWTConfig{Secret: jwtSecret, Issuer: tokenIssuer, TTL: tokenTTL},
		Password:  config.PasswordConfig{BcryptCost: bcrypt.MinCost},
		RateLimit: config.RateLimitConfig{APIPerMinute: unlimited, AuthPerMinute: unlimited},
		Cache:     config.CacheConfig{TaskListTTL: time.Minute},
		Outbox: config.OutboxConfig{
			PollInterval: 100 * time.Millisecond,
			BatchSize:    50,
			MaxAttempts:  5,
			Lease:        30 * time.Second,
		},
		Notify: config.NotifyConfig{WebhookURL: webhookURL, WebhookTimeout: 5 * time.Second},
	}
}

// newLogger returns the application logger: silent unless the tests run with -v.
func newLogger() *logger.Logger {
	cfg := logger.Config{Level: "info", Format: "text", Output: io.Discard}
	if testing.Verbose() {
		cfg.Output = os.Stderr
	}
	return logger.New(cfg)
}

// bootApp starts another instance of the application with cfg, next to the shared one, and
// returns it with a server for its handler. Both are released when the test ends; its outbox
// dispatcher is not run.
func bootApp(t *testing.T, cfg *config.Config) (*app.App, *apiClient) {
	t.Helper()
	require.NoError(t, cfg.Validate())
	instance, err := app.New(t.Context(), cfg, newLogger(), appVersion)
	require.NoError(t, err)
	server := httptest.NewServer(instance.Handler())
	t.Cleanup(func() {
		server.Close()
		assert.NoError(t, instance.Close())
	})
	return instance, clientFor(server)
}

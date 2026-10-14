// Package app wires the service together: it connects to MySQL and Redis, applies the
// migrations, builds the repositories, services and HTTP router, and runs the HTTP server until
// it is asked to shut down.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"

	"github.com/andrewhigh08/team-task-nexus/internal/config"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// App is a fully wired instance of the service. Build it with New, serve it with Run or
// through Handler, and release it with Close.
type App struct {
	cfg     *config.Config
	base    *logger.Logger // logger handed to every component
	log     *logger.Logger // base tagged with the app component
	version string

	db     *sqlx.DB
	rdb    *redis.Client
	router http.Handler
	srv    *http.Server

	closeOnce sync.Once
	closeErr  error
}

// New builds the application from cfg: it connects to MySQL and Redis, retrying each for up to
// DB_CONNECT_TIMEOUT while they start, applies the embedded migrations and prepares the HTTP
// server. Resources opened before a failure are released. version is logged when the server
// starts; a nil log means a default JSON logger.
func New(ctx context.Context, cfg *config.Config, log *logger.Logger, version string) (*App, error) {
	if cfg == nil {
		return nil, errors.New("app: configuration is required")
	}
	if log == nil {
		log = logger.New(logger.DefaultConfig())
	}
	a := &App{cfg: cfg, base: log, log: log.WithComponent("app"), version: version}
	if err := a.init(ctx); err != nil {
		return nil, errors.Join(err, a.Close())
	}
	return a, nil
}

func (a *App) init(ctx context.Context) error {
	if err := a.openMySQL(ctx); err != nil {
		return err
	}
	if err := a.openRedis(ctx); err != nil {
		return err
	}
	router, err := a.newRouter()
	if err != nil {
		return err
	}
	a.router = router
	a.srv = a.newServer()
	return nil
}

// newServer bounds every phase of a connection, so slow or idle clients cannot hold
// connections open indefinitely, and routes the server's own error log through slog.
func (a *App) newServer() *http.Server {
	return &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(a.cfg.Server.Port)),
		Handler:           a.router,
		ReadHeaderTimeout: a.cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       a.cfg.Server.ReadTimeout,
		WriteTimeout:      a.cfg.Server.WriteTimeout,
		IdleTimeout:       a.cfg.Server.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(a.base.WithComponent("http").Handler(), slog.LevelError),
	}
}

// Handler returns the HTTP handler of the application, for tests that serve it without Run.
func (a *App) Handler() http.Handler {
	return a.router
}

// Close releases the Redis client and the MySQL pool without touching the HTTP server. It is
// idempotent; Run calls it once the server has shut down.
func (a *App) Close() error {
	a.closeOnce.Do(func() { a.closeErr = a.release() })
	return a.closeErr
}

func (a *App) release() error {
	var errs []error
	if a.rdb != nil {
		if err := a.rdb.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close redis: %w", err))
		}
	}
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close mysql: %w", err))
		}
	}
	return errors.Join(errs...)
}

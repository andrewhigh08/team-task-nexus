package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

const (
	// Bounds of a single Redis network operation; DB_CONNECT_TIMEOUT bounds the startup retries.
	redisDialTimeout  = 5 * time.Second
	redisReadTimeout  = 3 * time.Second
	redisWriteTimeout = 3 * time.Second

	firstRetryDelay = 250 * time.Millisecond
	maxRetryDelay   = 5 * time.Second
)

// openMySQL opens the connection pool, waits until MySQL answers and applies the migrations.
func (a *App) openMySQL(ctx context.Context) error {
	connector, err := a.mysqlConnector(a.cfg.DSN())
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	migrationConnector, err := a.mysqlConnector(a.cfg.MigrationDSN())
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	db := sqlx.NewDb(sql.OpenDB(connector), "mysql")
	a.db = db
	db.SetMaxOpenConns(a.cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(a.cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(a.cfg.Database.ConnMaxLifetime)

	if err := a.waitFor(ctx, "mysql", db.PingContext); err != nil {
		return err
	}
	a.log.Info("mysql connection established", "host", a.cfg.Database.Host, "database", a.cfg.Database.Name)

	if err := migrateUp(ctx, migrationConnector); err != nil {
		return err
	}
	a.log.Info("database migrations applied")
	return nil
}

// mysqlConnector returns a connector for dsn whose driver messages, such as a broken connection
// being dropped, go to the log with the mysql component instead of to stderr. The logger is set
// on the connector because mysql.SetLogger would change it for every pool in the process.
func (a *App) mysqlConnector(dsn string) (driver.Connector, error) {
	mc, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	mc.Logger = logger.NewMySQLLogger(a.base.WithComponent("mysql"))
	return mysql.NewConnector(mc)
}

// openRedis creates the client and waits until Redis answers.
func (a *App) openRedis(ctx context.Context) error {
	a.rdb = redis.NewClient(&redis.Options{
		Addr:         a.cfg.Redis.Addr,
		Password:     a.cfg.Redis.Password,
		DB:           a.cfg.Redis.DB,
		DialTimeout:  redisDialTimeout,
		ReadTimeout:  redisReadTimeout,
		WriteTimeout: redisWriteTimeout,
	})
	ping := func(ctx context.Context) error { return a.rdb.Ping(ctx).Err() }
	if err := a.waitFor(ctx, "redis", ping); err != nil {
		return err
	}
	a.log.Info("redis connection established", "addr", a.cfg.Redis.Addr)
	return nil
}

// waitFor calls ping until it succeeds, giving up once DB_CONNECT_TIMEOUT has elapsed or ctx is
// done. Dependencies started next to the service, as in docker compose, may take a while to
// accept connections; the pause between attempts doubles up to maxRetryDelay.
func (a *App) waitFor(ctx context.Context, name string, ping func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Database.ConnectTimeout)
	defer cancel()

	delay := firstRetryDelay
	for attempt := 1; ; attempt++ {
		err := ping(ctx)
		if err == nil {
			return nil
		}
		a.log.Warn("waiting for dependency", "dependency", name, "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s not reachable after %d attempts: %w", name, attempt, err)
		case <-time.After(delay):
		}
		delay = min(2*delay, maxRetryDelay)
	}
}

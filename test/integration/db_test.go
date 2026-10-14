package integration

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	// Registers the "mysql" database/sql driver.
	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/config"
	"github.com/andrewhigh08/team-task-nexus/migrations"
)

// openDB opens a connection pool on the database cfg points at, with the DSN of the
// application, and closes it when the test ends.
func openDB(t *testing.T, cfg *config.Config) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("mysql", cfg.DSN())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	require.NoError(t, db.PingContext(t.Context()))
	return db
}

// sharedDB opens a connection pool on the database of the shared application, for checks the
// API does not answer and for data it cannot produce, such as tasks completed weeks ago.
func sharedDB(t *testing.T) *sqlx.DB {
	t.Helper()
	return openDB(t, env(t).cfg)
}

// scratchDatabase creates an empty database for a test that must not share the schema, or the
// outbox, of the application under test, and returns a configuration that points at it as
// root. The database is dropped when the test ends.
func scratchDatabase(t *testing.T) *config.Config {
	t.Helper()
	cfg := *env(t).cfg
	cfg.Database.User = mysqlRootUser
	admin := openDB(t, &cfg)
	name := strings.ReplaceAll(uniqueName(t), "-", "_")
	_, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+name)
	require.NoError(t, err)
	t.Cleanup(func() {
		// The context of the test is already canceled when cleanups run.
		_, dropErr := admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name)
		assert.NoError(t, dropErr)
	})
	cfg.Database.Name = name
	return &cfg
}

// migratedDatabase is scratchDatabase with every migration applied.
func migratedDatabase(t *testing.T) *config.Config {
	t.Helper()
	cfg := scratchDatabase(t)
	require.NoError(t, newMigrator(t, cfg).Up())
	return cfg
}

// newMigrator returns golang-migrate on the embedded migrations and the database cfg points
// at, as the application runs it at startup. Its connection is closed when the test ends.
func newMigrator(t *testing.T, cfg *config.Config) *migrate.Migrate {
	t.Helper()
	db, err := sql.Open("mysql", cfg.MigrationDSN())
	require.NoError(t, err)
	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	require.NoError(t, err)
	source, err := iofs.New(migrations.FS, ".")
	require.NoError(t, err)
	m, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	require.NoError(t, err)
	t.Cleanup(func() {
		sourceErr, dbErr := m.Close()
		assert.NoError(t, errors.Join(sourceErr, dbErr))
	})
	return m
}

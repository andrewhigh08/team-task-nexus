package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/andrewhigh08/team-task-nexus/migrations"
)

// migrateUp applies the embedded migrations the database has not seen yet, over a connection
// pool of its own opened with connector and closed before returning (see config.MigrationDSN).
// golang-migrate takes no context: canceling ctx stops it between two migration files, never
// inside one.
func migrateUp(ctx context.Context, connector driver.Connector) (err error) {
	m, err := newMigrate(connector)
	if err != nil {
		return err
	}
	defer func() {
		sourceErr, dbErr := m.Close()
		err = errors.Join(err, sourceErr, dbErr)
	}()

	stop := context.AfterFunc(ctx, func() { m.GracefulStop <- true })
	defer stop()

	upErr := m.Up()
	if errors.Is(upErr, migrate.ErrNoChange) {
		upErr = nil
	}
	if upErr == nil {
		// A graceful stop ends Up without an error, too.
		upErr = ctx.Err()
	}
	if upErr != nil {
		return fmt.Errorf("apply migrations: %w", upErr)
	}
	return nil
}

// newMigrate prepares golang-migrate on the embedded source and a new pool opened with
// connector; closing the result closes the pool.
func newMigrate(connector driver.Connector) (*migrate.Migrate, error) {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	db := sql.OpenDB(connector)
	dbDriver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("prepare migration driver: %w", err), source.Close(), db.Close())
	}
	m, err := migrate.NewWithInstance("iofs", source, "mysql", dbDriver)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("prepare migrations: %w", err), source.Close(), dbDriver.Close())
	}
	return m, nil
}

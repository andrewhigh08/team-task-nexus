package integration

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// latestMigration is the version of the newest file in migrations/.
const latestMigration = 8

// appTables are the tables the migrations create.
var appTables = []string{"outbox", "task_comments", "task_history", "tasks", "team_members", "teams", "users"}

func TestMigrationsRollBackAndReapplyCleanly(t *testing.T) {
	t.Parallel()
	cfg := scratchDatabase(t)
	db := openDB(t, cfg)
	m := newMigrator(t, cfg)

	require.NoError(t, m.Up())
	assertVersion(t, m, latestMigration)
	assert.Equal(t, appTables, tables(t, db))
	// An up-to-date schema is left alone.
	require.ErrorIs(t, m.Up(), migrate.ErrNoChange)

	require.NoError(t, m.Down())
	_, _, err := m.Version()
	require.ErrorIs(t, err, migrate.ErrNilVersion)
	assert.Empty(t, tables(t, db), "every down migration undoes its up migration")

	require.NoError(t, m.Up())
	assertVersion(t, m, latestMigration)
	assert.Equal(t, appTables, tables(t, db))
}

func assertVersion(t *testing.T, m *migrate.Migrate, want uint) {
	t.Helper()
	version, dirty, err := m.Version()
	require.NoError(t, err)
	assert.Equal(t, want, version)
	assert.False(t, dirty)
}

// tables lists the tables of the database db is connected to, by name, leaving out the one
// golang-migrate keeps its version in.
func tables(t *testing.T, db *sqlx.DB) []string {
	t.Helper()
	var names []string
	require.NoError(t, db.SelectContext(t.Context(), &names, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name <> 'schema_migrations'
		ORDER BY table_name`))
	return names
}

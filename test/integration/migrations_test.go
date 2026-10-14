package integration

import (
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// latestMigration is the version of the newest file in migrations/.
const latestMigration = 9

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

func TestEmailCollationMigrationLowerCasesStoredEmails(t *testing.T) {
	t.Parallel()
	cfg := scratchDatabase(t)
	db := openDB(t, cfg)
	m := newMigrator(t, cfg)
	// Migration 000009 changes the collation of users.email.
	require.NoError(t, m.Migrate(8))

	// The first release stored emails as typed.
	edited := time.Date(2025, time.March, 1, 12, 0, 0, 0, time.UTC)
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO users (email, password_hash, full_name, updated_at)
		VALUES ('Mixed.Case@Example.com', 'hash', 'Mixed', ?), ('lower@example.com', 'hash', 'Lower', ?)`,
		edited, edited)
	require.NoError(t, err)

	require.NoError(t, m.Up())
	type userRow struct {
		Email     string    `db:"email"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	var users []userRow
	require.NoError(t, db.SelectContext(t.Context(), &users, "SELECT email, updated_at FROM users ORDER BY id"))
	assert.Equal(t, []userRow{
		{Email: "mixed.case@example.com", UpdatedAt: edited},
		{Email: "lower@example.com", UpdatedAt: edited},
	}, users, "the repair is not an edit")

	var matches int
	require.NoError(t, db.GetContext(t.Context(), &matches, "SELECT COUNT(*) FROM users WHERE email = 'Mixed.Case@Example.com'"))
	assert.Zero(t, matches, "emails are compared byte for byte")
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

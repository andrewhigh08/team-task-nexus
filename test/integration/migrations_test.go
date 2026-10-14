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

func TestEachDownMigrationRestoresThePreviousSchema(t *testing.T) {
	t.Parallel()
	cfg := scratchDatabase(t)
	db := openDB(t, cfg)
	m := newMigrator(t, cfg)

	// schemas[v] is the schema at version v; version 0 is the empty database.
	schemas := make([]schema, latestMigration+1)
	schemas[0] = schemaOf(t, db)
	for version := 1; version <= latestMigration; version++ {
		require.NoError(t, m.Steps(1), "up migration %d", version)
		assertVersion(t, m, uint(version))
		schemas[version] = schemaOf(t, db)
	}
	assert.Equal(t, appTables, tables(t, db))
	// An up-to-date schema is left alone.
	require.ErrorIs(t, m.Up(), migrate.ErrNoChange)

	// The API takes any calendar date as a due date, while down migration 000007 turns due_date
	// back into a TIMESTAMP, which holds 1970-01-01 00:00:01 to 2038-01-19 03:14:07 UTC.
	edited := time.Date(2025, time.March, 1, 12, 0, 0, 0, time.UTC)
	insertTasks(t, db, edited, "2040-01-01", "1970-01-01", "2030-05-05")
	stepDown(t, m, db, schemas, latestMigration, 6)
	type taskRow struct {
		Title     string     `db:"title"`
		DueDate   *time.Time `db:"due_date"`
		UpdatedAt time.Time  `db:"updated_at"`
	}
	var tasks []taskRow
	require.NoError(t, db.SelectContext(t.Context(), &tasks, "SELECT title, due_date, updated_at FROM tasks ORDER BY id"))
	kept := time.Date(2030, time.May, 5, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, []taskRow{
		{Title: "2040-01-01", DueDate: nil, UpdatedAt: edited},
		{Title: "1970-01-01", DueDate: nil, UpdatedAt: edited},
		{Title: "2030-05-05", DueDate: &kept, UpdatedAt: edited},
	}, tasks, "due dates a TIMESTAMP cannot hold are cleared, and the repair is not an edit")

	stepDown(t, m, db, schemas, 6, 0)
	_, _, err := m.Version()
	require.ErrorIs(t, err, migrate.ErrNilVersion)
	assert.Empty(t, tables(t, db))

	require.NoError(t, m.Up())
	assertVersion(t, m, latestMigration)
	assert.Equal(t, schemas[latestMigration], schemaOf(t, db), "the migrations apply again after a full rollback")
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

// stepDown applies the down migrations from version from to version to one at a time and
// requires after each that the schema is the one schemas holds for the version below it. Once
// the schema has drifted, the migrations below cannot be judged, so the test stops there.
func stepDown(t *testing.T, m *migrate.Migrate, db *sqlx.DB, schemas []schema, from, to int) {
	t.Helper()
	for version := from; version > to; version-- {
		require.NoError(t, m.Steps(-1), "down migration %d", version)
		require.Equal(t, schemas[version-1], schemaOf(t, db),
			"down migration %d restores the schema of version %d", version, version-1)
	}
}

// insertTasks stores, in the schema of the latest version, a task due on each of dates, titled
// with its date and last edited at edited, in a team of its own.
func insertTasks(t *testing.T, db *sqlx.DB, edited time.Time, dates ...string) {
	t.Helper()
	res, err := db.ExecContext(t.Context(),
		"INSERT INTO users (email, password_hash, full_name) VALUES ('owner@example.com', 'hash', 'Owner')")
	require.NoError(t, err)
	ownerID, err := res.LastInsertId()
	require.NoError(t, err)
	res, err = db.ExecContext(t.Context(), "INSERT INTO teams (name, description, owner_id) VALUES ('Team', '', ?)", ownerID)
	require.NoError(t, err)
	teamID, err := res.LastInsertId()
	require.NoError(t, err)
	for _, date := range dates {
		_, err = db.ExecContext(t.Context(), `
			INSERT INTO tasks (title, description, team_id, creator_id, due_date, updated_at)
			VALUES (?, '', ?, ?, ?, ?)`, date, teamID, ownerID, date, edited)
		require.NoError(t, err)
	}
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

// schema is what information_schema reports about the tables of a database, in an order that
// does not depend on the order in which columns, indexes and constraints were added or
// dropped. A down migration has to restore all of it.
type schema struct {
	Columns     []schemaColumn
	Indexes     []schemaIndexColumn
	Constraints []schemaConstraint
}

// schemaColumn is a column of a table, in the position it has in the table.
type schemaColumn struct {
	Table     string  `db:"table_name"`
	Name      string  `db:"column_name"`
	Type      string  `db:"column_type"`
	Nullable  string  `db:"is_nullable"`
	Default   *string `db:"column_default"`
	Extra     string  `db:"extra"`
	Collation *string `db:"collation_name"`
}

// schemaIndexColumn is a column of an index, at its position in the index.
type schemaIndexColumn struct {
	Table    string `db:"table_name"`
	Index    string `db:"index_name"`
	Position int    `db:"seq_in_index"`
	Column   string `db:"column_name"`
	Unique   bool   `db:"is_unique"`
}

// schemaConstraint is a primary key, unique, foreign key or check constraint of a table. Check
// is the condition of a check constraint; References and OnDelete describe a foreign key.
type schemaConstraint struct {
	Table      string  `db:"table_name"`
	Name       string  `db:"constraint_name"`
	Type       string  `db:"constraint_type"`
	Check      *string `db:"check_clause"`
	References *string `db:"referenced_table_name"`
	OnDelete   *string `db:"delete_rule"`
}

// schemaOf reads the schema of the database db is connected to, leaving out the table
// golang-migrate keeps its version in. information_schema names its columns in upper case, so
// every one is selected under the name its struct tag gives.
func schemaOf(t *testing.T, db *sqlx.DB) schema {
	t.Helper()
	var s schema
	require.NoError(t, db.SelectContext(t.Context(), &s.Columns, `
		SELECT table_name AS table_name, column_name AS column_name, column_type AS column_type,
			is_nullable AS is_nullable, column_default AS column_default, extra AS extra,
			collation_name AS collation_name
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name <> 'schema_migrations'
		ORDER BY table_name, ordinal_position`))
	require.NoError(t, db.SelectContext(t.Context(), &s.Indexes, `
		SELECT table_name AS table_name, index_name AS index_name, seq_in_index AS seq_in_index,
			column_name AS column_name, non_unique = 0 AS is_unique
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name <> 'schema_migrations'
		ORDER BY table_name, index_name, seq_in_index`))
	require.NoError(t, db.SelectContext(t.Context(), &s.Constraints, `
		SELECT tc.table_name AS table_name, tc.constraint_name AS constraint_name,
			tc.constraint_type AS constraint_type, cc.check_clause AS check_clause,
			rc.referenced_table_name AS referenced_table_name, rc.delete_rule AS delete_rule
		FROM information_schema.table_constraints AS tc
		LEFT JOIN information_schema.check_constraints AS cc
			ON cc.constraint_schema = tc.constraint_schema AND cc.constraint_name = tc.constraint_name
		LEFT JOIN information_schema.referential_constraints AS rc
			ON rc.constraint_schema = tc.constraint_schema AND rc.constraint_name = tc.constraint_name
		WHERE tc.table_schema = DATABASE() AND tc.table_name <> 'schema_migrations'
		ORDER BY tc.table_name, tc.constraint_name`))
	return s
}

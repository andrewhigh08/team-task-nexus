package mysqlrepo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const claimQuery = "SELECT id, event_type, payload, attempts, created_at " +
	"FROM outbox FORCE INDEX (idx_outbox_pending) " +
	"WHERE sent_at IS NULL AND failed_at IS NULL AND next_attempt_at <= UTC_TIMESTAMP(3) " +
	"ORDER BY next_attempt_at, id LIMIT ? FOR UPDATE SKIP LOCKED"

// sqlPattern matches query whatever the whitespace of the statement.
func sqlPattern(query string) string {
	return strings.ReplaceAll(regexp.QuoteMeta(query), " ", `\s+`)
}

var claimPattern = sqlPattern(claimQuery)

func outboxRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "event_type", "payload", "attempts", "created_at"})
}

func TestOutboxRepo_Add(t *testing.T) {
	t.Parallel()

	t.Run("one statement for all events, in the transaction", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(sqlPattern("INSERT INTO outbox (event_type, payload, created_at, next_attempt_at) VALUES "+
			"(?, ?, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)),(?, ?, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))")).
			WithArgs("task.assigned", `{"task_id":4}`, "task.commented", `{"comment_id":9}`).
			WillReturnResult(sqlmock.NewResult(30, 2))
		mock.ExpectCommit()

		err := NewTxManager(db).WithinTx(t.Context(), func(ctx context.Context) error {
			return NewOutboxRepo(db).Add(ctx,
				domain.NewEvent{Type: domain.EventTaskAssigned, Payload: []byte(`{"task_id":4}`)},
				domain.NewEvent{Type: domain.EventTaskCommented, Payload: []byte(`{"comment_id":9}`)},
			)
		})

		require.NoError(t, err)
	})

	t.Run("nothing to add", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectCommit()

		err := NewTxManager(db).WithinTx(t.Context(), func(ctx context.Context) error {
			return NewOutboxRepo(db).Add(ctx)
		})

		require.NoError(t, err)
	})

	t.Run("outside a transaction", func(t *testing.T) {
		t.Parallel()
		db, _ := newMockDB(t)

		// No statement is expected: an event must not outlive a rolled back change.
		err := NewOutboxRepo(db).Add(t.Context(), domain.NewEvent{Type: domain.EventTaskAssigned, Payload: []byte(`{}`)})

		requireCode(t, err, apperror.CodeInternal)
	})
}

func TestOutboxRepo_Claim(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 3, 14, 9, 26, 53, 123_000_000, time.UTC)

	t.Run("leases the due events and counts the attempt", func(t *testing.T) {
		t.Parallel()
		db, mock, tx := newTxOptionsMockDB(t)
		mock.ExpectBegin()
		mock.ExpectQuery(claimPattern).WithArgs(50).WillReturnRows(outboxRows().
			AddRow(3, "task.assigned", []byte(`{"task_id": 4}`), 0, created).
			AddRow(8, "task.commented", []byte(`{"comment_id": 9}`), 2, created))
		mock.ExpectExec(regexp.QuoteMeta("SET attempts = attempts + 1, next_attempt_at = UTC_TIMESTAMP(3) + INTERVAL ? MICROSECOND")+
			`\s+WHERE id IN \(\?, \?\)`).
			WithArgs(int64(30_000_000), 3, 8).
			WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectCommit()

		events, err := NewOutboxRepo(db).Claim(t.Context(), 50, 30*time.Second)

		require.NoError(t, err)
		assert.Equal(t, []domain.OutboxEvent{
			{ID: 3, Type: domain.EventTaskAssigned, Payload: []byte(`{"task_id": 4}`), Attempts: 1, CreatedAt: created},
			{ID: 8, Type: domain.EventTaskCommented, Payload: []byte(`{"comment_id": 9}`), Attempts: 3, CreatedAt: created},
		}, events)
		// Without gap locks, so the claim never holds up the INSERT of an event.
		assert.Equal(t, []driver.TxOptions{{Isolation: driver.IsolationLevel(sql.LevelReadCommitted)}}, tx.begun)
	})

	t.Run("nothing due", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectQuery(claimPattern).WithArgs(10).WillReturnRows(outboxRows())
		mock.ExpectCommit()

		events, err := NewOutboxRepo(db).Claim(t.Context(), 10, time.Second)

		require.NoError(t, err)
		assert.Empty(t, events)
	})

	t.Run("a failed lease claims nothing", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		boom := errors.New("lock wait timeout exceeded")
		mock.ExpectBegin()
		mock.ExpectQuery(claimPattern).WithArgs(10).WillReturnRows(outboxRows().AddRow(3, "task.assigned", []byte(`{}`), 0, created))
		mock.ExpectExec("UPDATE outbox").WillReturnError(boom)
		mock.ExpectRollback()

		events, err := NewOutboxRepo(db).Claim(t.Context(), 10, time.Second)

		require.ErrorIs(t, err, boom)
		requireCode(t, err, apperror.CodeInternal)
		assert.Nil(t, events)
	})

	t.Run("a failed query claims nothing", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		boom := errors.New("connection reset by peer")
		mock.ExpectBegin()
		mock.ExpectQuery(claimPattern).WillReturnError(boom)
		mock.ExpectRollback()

		events, err := NewOutboxRepo(db).Claim(t.Context(), 10, time.Second)

		require.ErrorIs(t, err, boom)
		assert.Nil(t, events)
	})
}

func TestOutboxRepo_Release(t *testing.T) {
	t.Parallel()
	const releaseQuery = "UPDATE outbox SET attempts = attempts - 1, next_attempt_at = UTC_TIMESTAMP(3) " +
		"WHERE id IN (?, ?) AND sent_at IS NULL AND failed_at IS NULL"

	t.Run("takes back the attempt of every event", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		mock.ExpectExec(sqlPattern(releaseQuery)).WithArgs(6, 7).WillReturnResult(sqlmock.NewResult(0, 2))

		require.NoError(t, NewOutboxRepo(db).Release(t.Context(), []int64{6, 7}))
	})

	t.Run("nothing to release", func(t *testing.T) {
		t.Parallel()
		db, _ := newMockDB(t)

		require.NoError(t, NewOutboxRepo(db).Release(t.Context(), nil))
	})

	t.Run("a failed update", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		boom := errors.New("connection reset by peer")
		mock.ExpectExec("UPDATE outbox").WillReturnError(boom)

		err := NewOutboxRepo(db).Release(t.Context(), []int64{6, 7})

		require.ErrorIs(t, err, boom)
		requireCode(t, err, apperror.CodeInternal)
	})
}

func TestOutboxRepo_Mark(t *testing.T) {
	t.Parallel()
	next := time.Date(2026, 3, 14, 17, 27, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	longError := strings.Repeat("é", maxLastErrorChars) + "cut off"
	tests := []struct {
		name  string
		mark  func(ctx context.Context, r *OutboxRepo) error
		query string
		args  []driver.Value
	}{
		{
			name:  "sent",
			mark:  func(ctx context.Context, r *OutboxRepo) error { return r.MarkSent(ctx, 3) },
			query: "UPDATE outbox SET sent_at = UTC_TIMESTAMP(3) WHERE id = ?",
			args:  []driver.Value{3},
		},
		{
			name: "retry, at the given instant in UTC",
			mark: func(ctx context.Context, r *OutboxRepo) error {
				return r.MarkRetry(ctx, 3, "webhook answered 503", next)
			},
			query: "UPDATE outbox SET last_error = ?, next_attempt_at = ? WHERE id = ?",
			args:  []driver.Value{"webhook answered 503", next.UTC(), 3},
		},
		{
			name:  "retry with an error longer than its column",
			mark:  func(ctx context.Context, r *OutboxRepo) error { return r.MarkRetry(ctx, 3, longError, next) },
			query: "UPDATE outbox SET last_error = ?, next_attempt_at = ? WHERE id = ?",
			args:  []driver.Value{strings.Repeat("é", maxLastErrorChars), next.UTC(), 3},
		},
		{
			name: "retry with an error that is not valid UTF-8",
			mark: func(ctx context.Context, r *OutboxRepo) error {
				return r.MarkRetry(ctx, 3, "webhook answered 503 Unavailable\xff\xfe now", next)
			},
			query: "UPDATE outbox SET last_error = ?, next_attempt_at = ? WHERE id = ?",
			args:  []driver.Value{"webhook answered 503 Unavailable\uFFFD now", next.UTC(), 3},
		},
		{
			name:  "failed",
			mark:  func(ctx context.Context, r *OutboxRepo) error { return r.MarkFailed(ctx, 3, longError) },
			query: "UPDATE outbox SET last_error = ?, failed_at = UTC_TIMESTAMP(3) WHERE id = ?",
			args:  []driver.Value{strings.Repeat("é", maxLastErrorChars), 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock := newMockDB(t)
			mock.ExpectExec(regexp.QuoteMeta(tt.query)).WithArgs(tt.args...).WillReturnResult(sqlmock.NewResult(0, 1))

			require.NoError(t, tt.mark(t.Context(), NewOutboxRepo(db)))
		})
	}
}

func TestOutboxRepo_Mark_MissingEvent(t *testing.T) {
	t.Parallel()
	db, mock := newMockDB(t)
	mock.ExpectExec("UPDATE outbox").WillReturnResult(sqlmock.NewResult(0, 0))

	err := NewOutboxRepo(db).MarkSent(t.Context(), 3)

	requireCode(t, err, apperror.CodeNotFound)
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    string
		want string
	}{
		{name: "shorter", s: "abc", want: "abc"},
		{name: "exactly as long", s: "abcd", want: "abcd"},
		{name: "longer", s: "abcde", want: "abcd"},
		{name: "counts characters, not bytes", s: "ёжик в тумане", want: "ёжик"},
		{name: "empty", s: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, truncate(tt.s, 4))
		})
	}
}

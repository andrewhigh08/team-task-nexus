package mysqlrepo

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const historyListQuery = "SELECT id, task_id, user_id, field, old_value, new_value, changed_at FROM task_history " +
	"WHERE task_id = ? ORDER BY changed_at DESC, id DESC LIMIT ? OFFSET ?"

func TestTaskHistoryRepo_Add(t *testing.T) {
	t.Parallel()

	t.Run("one statement for all changes", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		mock.ExpectExec(regexp.QuoteMeta("VALUES (?, ?, ?, ?, ?),(?, ?, ?, ?, ?)")).
			WithArgs(4, 3, "status", "todo", "done", 4, 3, "assignee_id", "", "3").
			WillReturnResult(sqlmock.NewResult(10, 2))

		err := NewTaskHistoryRepo(db).Add(t.Context(), []domain.TaskChange{
			{TaskID: 4, UserID: 3, Field: "status", OldValue: "todo", NewValue: "done"},
			{TaskID: 4, UserID: 3, Field: "assignee_id", OldValue: "", NewValue: "3"},
		})

		require.NoError(t, err)
	})

	t.Run("nothing to add", func(t *testing.T) {
		t.Parallel()
		db, _ := newMockDB(t)

		require.NoError(t, NewTaskHistoryRepo(db).Add(t.Context(), nil))
	})
}

func TestTaskHistoryRepo_ListByTask(t *testing.T) {
	t.Parallel()
	changedAt := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	tests := []struct {
		name   string
		p      domain.PageRequest
		offset int
	}{
		{name: "first page", p: domain.PageRequest{Page: 1, PageSize: 20}},
		{name: "later page", p: domain.PageRequest{Page: 3, PageSize: 10}, offset: 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock := newMockDB(t)
			mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM task_history WHERE task_id = ?")).
				WithArgs(4).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(23))
			mock.ExpectQuery(sqlPattern(historyListQuery)).
				WithArgs(4, tt.p.PageSize, tt.offset).
				WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "user_id", "field", "old_value", "new_value", "changed_at"}).
					AddRow(11, 4, 3, "status", "todo", "done", changedAt).
					AddRow(10, 4, 3, "assignee_id", "", "3", changedAt))

			changes, total, err := NewTaskHistoryRepo(db).ListByTask(t.Context(), 4, tt.p)

			require.NoError(t, err)
			assert.Equal(t, 23, total)
			assert.Equal(t, []domain.TaskChange{
				{ID: 11, TaskID: 4, UserID: 3, Field: "status", OldValue: "todo", NewValue: "done", ChangedAt: changedAt},
				{ID: 10, TaskID: 4, UserID: 3, Field: "assignee_id", OldValue: "", NewValue: "3", ChangedAt: changedAt},
			}, changes)
		})
	}

	t.Run("unnormalized page", func(t *testing.T) {
		t.Parallel()
		db, _ := newMockDB(t)

		// No query is expected: the page is rejected before reaching the database.
		changes, total, err := NewTaskHistoryRepo(db).ListByTask(t.Context(), 4, domain.PageRequest{Page: 1})

		requireCode(t, err, apperror.CodeInternal)
		assert.Nil(t, changes)
		assert.Zero(t, total)
	})

	t.Run("failed count", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM task_history")).WillReturnError(errors.New("connection reset"))

		changes, total, err := NewTaskHistoryRepo(db).ListByTask(t.Context(), 4, domain.PageRequest{Page: 1, PageSize: 20})

		requireCode(t, err, apperror.CodeInternal)
		assert.Nil(t, changes)
		assert.Zero(t, total)
	})
}

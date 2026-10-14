package mysqlrepo

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

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

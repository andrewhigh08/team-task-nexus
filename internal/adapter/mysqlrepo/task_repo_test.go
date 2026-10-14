package mysqlrepo

import (
	"context"
	"database/sql/driver"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const scopedWhere = "WHERE team_id IN (SELECT team_id FROM team_members WHERE user_id = ?)"

var (
	created = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	due     = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
)

func taskRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "team_id", "title", "description", "status", "priority", "creator_id", "assignee_id",
		"due_date", "completed_at", "created_at", "updated_at",
	})
}

func TestTaskRepo_List_IsAlwaysScopedToTheViewer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		q      domain.TaskQuery
		where  string
		args   []driver.Value
		offset int
	}{
		{
			name:  "no filter",
			q:     domain.TaskQuery{ViewerID: 42, Page: 1, PageSize: 20},
			where: scopedWhere,
			args:  []driver.Value{int64(42)},
		},
		{
			name:   "every filter",
			q:      domain.TaskQuery{ViewerID: 42, TeamID: 7, Status: domain.TaskStatusReview, AssigneeID: 3, Page: 3, PageSize: 10},
			where:  scopedWhere + " AND team_id = ? AND status = ? AND assignee_id = ?",
			args:   []driver.Value{int64(42), int64(7), "review", int64(3)},
			offset: 20,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock := newMockDB(t)
			mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM tasks " + tt.where)).
				WithArgs(tt.args...).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(21))
			mock.ExpectQuery(regexp.QuoteMeta("SELECT " + taskColumns + " FROM tasks " + tt.where + " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?")).
				WithArgs(append(tt.args, tt.q.PageSize, tt.offset)...).
				WillReturnRows(taskRows().
					AddRow(5, 7, "Open", "", "todo", 2, 1, nil, nil, nil, created, created).
					AddRow(4, 7, "Done", "d", "done", 3, 1, 3, due, created, created, created))

			tasks, total, err := NewTaskRepo(db).List(t.Context(), tt.q)

			require.NoError(t, err)
			assert.Equal(t, 21, total)
			assert.Equal(t, []domain.Task{
				{
					ID: 5, TeamID: 7, Title: "Open", Status: domain.TaskStatusTodo, Priority: domain.TaskPriorityMedium,
					CreatorID: 1, CreatedAt: created, UpdatedAt: created,
				},
				{
					ID: 4, TeamID: 7, Title: "Done", Description: "d", Status: domain.TaskStatusDone, Priority: domain.TaskPriorityHigh,
					CreatorID: 1, AssigneeID: new(int64(3)), DueDate: &due, CompletedAt: &created, CreatedAt: created, UpdatedAt: created,
				},
			}, tasks)
		})
	}
}

func TestTaskRepo_List_RejectsAnUnnormalizedPage(t *testing.T) {
	t.Parallel()
	db, _ := newMockDB(t)

	// No query is expected: the page is rejected before reaching the database.
	tasks, total, err := NewTaskRepo(db).List(t.Context(), domain.TaskQuery{ViewerID: 42, Page: 0, PageSize: 20})

	requireCode(t, err, apperror.CodeInternal)
	assert.Nil(t, tasks)
	assert.Zero(t, total)
}

func TestTaskRepo_GetByIDForUpdate(t *testing.T) {
	t.Parallel()

	t.Run("outside a transaction", func(t *testing.T) {
		t.Parallel()
		db, _ := newMockDB(t)

		task, err := NewTaskRepo(db).GetByIDForUpdate(t.Context(), 4)

		requireCode(t, err, apperror.CodeInternal)
		assert.Nil(t, task)
	})

	t.Run("locks the row inside the transaction", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT " + taskColumns + " FROM tasks WHERE id = ? FOR UPDATE")).
			WithArgs(4).
			WillReturnRows(taskRows().AddRow(4, 7, "Open", "", "todo", 2, 1, nil, nil, nil, created, created))
		mock.ExpectCommit()

		var task *domain.Task
		err := NewTxManager(db).WithinTx(t.Context(), func(ctx context.Context) error {
			var err error
			task, err = NewTaskRepo(db).GetByIDForUpdate(ctx, 4)
			return err
		})

		require.NoError(t, err)
		assert.Equal(t, int64(4), task.ID)
	})

	t.Run("missing task", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectQuery("FOR UPDATE").WithArgs(4).WillReturnRows(taskRows())
		mock.ExpectRollback()

		err := NewTxManager(db).WithinTx(t.Context(), func(ctx context.Context) error {
			_, err := NewTaskRepo(db).GetByIDForUpdate(ctx, 4)
			return err
		})

		requireCode(t, err, apperror.CodeNotFound)
	})
}

func TestTaskRepo_Update(t *testing.T) {
	t.Parallel()
	task := &domain.Task{
		ID: 4, Title: "Done", Description: "d", Status: domain.TaskStatusDone, Priority: domain.TaskPriorityHigh,
		AssigneeID: new(int64(3)), DueDate: &due, CompletedAt: &created,
	}
	tests := []struct {
		name     string
		task     *domain.Task
		args     []driver.Value
		affected int64
		wantCode string
	}{
		{
			name:     "every mutable column",
			task:     task,
			args:     []driver.Value{"Done", "d", "done", 3, int64(3), "2026-04-01", created, int64(4)},
			affected: 1,
		},
		{
			name:     "cleared optional columns",
			task:     &domain.Task{ID: 4, Title: "Open", Status: domain.TaskStatusTodo, Priority: domain.TaskPriorityLow},
			args:     []driver.Value{"Open", "", "todo", 1, nil, nil, nil, int64(4)},
			affected: 1,
		},
		{name: "missing task", task: task, args: nil, wantCode: apperror.CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock := newMockDB(t)
			exec := mock.ExpectExec(regexp.QuoteMeta("UPDATE tasks"))
			if tt.args != nil {
				exec.WithArgs(tt.args...)
			}
			exec.WillReturnResult(sqlmock.NewResult(0, tt.affected))

			err := NewTaskRepo(db).Update(t.Context(), tt.task)

			if tt.wantCode != "" {
				requireCode(t, err, tt.wantCode)
				return
			}
			require.NoError(t, err)
		})
	}
}

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

func TestCommentRepo_ListByTask(t *testing.T) {
	t.Parallel()
	posted := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	edited := posted.Add(time.Hour)
	tests := []struct {
		name   string
		p      domain.PageRequest
		offset int
	}{
		{name: "first page", p: domain.PageRequest{Page: 1, PageSize: 20}},
		{name: "later page", p: domain.PageRequest{Page: 2, PageSize: 100}, offset: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock := newMockDB(t)
			mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM task_comments WHERE task_id = ?")).
				WithArgs(4).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(102))
			mock.ExpectQuery(regexp.QuoteMeta("SELECT "+commentColumns+" FROM task_comments WHERE task_id = ? "+
				"ORDER BY created_at ASC, id ASC LIMIT ? OFFSET ?")).
				WithArgs(4, tt.p.PageSize, tt.offset).
				WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "user_id", "content", "created_at", "updated_at"}).
					AddRow(8, 4, 3, "First", posted, posted).
					AddRow(9, 4, 2, "Second", posted, edited))

			comments, total, err := NewCommentRepo(db).ListByTask(t.Context(), 4, tt.p)

			require.NoError(t, err)
			assert.Equal(t, 102, total)
			assert.Equal(t, []domain.Comment{
				{ID: 8, TaskID: 4, UserID: 3, Content: "First", CreatedAt: posted, UpdatedAt: posted},
				{ID: 9, TaskID: 4, UserID: 2, Content: "Second", CreatedAt: posted, UpdatedAt: edited},
			}, comments)
		})
	}

	t.Run("unnormalized page", func(t *testing.T) {
		t.Parallel()
		db, _ := newMockDB(t)

		// No query is expected: the page is rejected before reaching the database.
		comments, total, err := NewCommentRepo(db).ListByTask(t.Context(), 4, domain.PageRequest{PageSize: 20})

		requireCode(t, err, apperror.CodeInternal)
		assert.Nil(t, comments)
		assert.Zero(t, total)
	})

	t.Run("failed query", func(t *testing.T) {
		t.Parallel()
		db, mock := newMockDB(t)
		mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM task_comments")).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("FROM task_comments WHERE task_id = ? ORDER BY")).WillReturnError(errors.New("connection reset"))

		comments, total, err := NewCommentRepo(db).ListByTask(t.Context(), 4, domain.PageRequest{Page: 1, PageSize: 20})

		requireCode(t, err, apperror.CodeInternal)
		assert.Nil(t, comments)
		assert.Zero(t, total)
	})
}

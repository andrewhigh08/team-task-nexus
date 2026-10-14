package mysqlrepo

import (
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

func TestTeamRepo_GetMember(t *testing.T) {
	t.Parallel()
	query := regexp.QuoteMeta("SELECT team_id, user_id, role FROM team_members WHERE team_id = ? AND user_id = ?")
	tests := []struct {
		name     string
		result   func(*sqlmock.ExpectedQuery)
		want     *domain.TeamMember
		wantCode string
	}{
		{
			name: "member",
			result: func(q *sqlmock.ExpectedQuery) {
				q.WillReturnRows(sqlmock.NewRows([]string{"team_id", "user_id", "role"}).AddRow(7, 3, "admin"))
			},
			want: &domain.TeamMember{TeamID: 7, UserID: 3, Role: domain.TeamRoleAdmin},
		},
		{
			name:     "not a member is a not found, never a nil member",
			result:   func(q *sqlmock.ExpectedQuery) { q.WillReturnError(sql.ErrNoRows) },
			wantCode: apperror.CodeNotFound,
		},
		{
			name:     "failing database",
			result:   func(q *sqlmock.ExpectedQuery) { q.WillReturnError(errors.New("connection reset")) },
			wantCode: apperror.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock := newMockDB(t)
			tt.result(mock.ExpectQuery(query).WithArgs(7, 3))

			member, err := NewTeamRepo(db).GetMember(t.Context(), 7, 3)

			if tt.wantCode != "" {
				requireCode(t, err, tt.wantCode)
				assert.Nil(t, member)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, member)
		})
	}
}

func TestTeamRepo_AddMember_DuplicateIsAConflict(t *testing.T) {
	t.Parallel()
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)")).
		WithArgs(7, 3, "member").
		WillReturnError(&mysql.MySQLError{Number: erDupEntry, Message: "Duplicate entry '7-3'"})

	err := NewTeamRepo(db).AddMember(t.Context(), domain.TeamMember{TeamID: 7, UserID: 3, Role: domain.TeamRoleMember})

	requireCode(t, err, apperror.CodeConflict)
}

func TestTeamRepo_RemoveMember(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		affected int64
		wantCode string
	}{
		{name: "removed", affected: 1},
		{name: "not a member", affected: 0, wantCode: apperror.CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock := newMockDB(t)
			mock.ExpectExec(regexp.QuoteMeta("DELETE FROM team_members WHERE team_id = ? AND user_id = ?")).
				WithArgs(7, 3).
				WillReturnResult(sqlmock.NewResult(0, tt.affected))

			err := NewTeamRepo(db).RemoveMember(t.Context(), 7, 3)

			if tt.wantCode != "" {
				requireCode(t, err, tt.wantCode)
				return
			}
			require.NoError(t, err)
		})
	}
}

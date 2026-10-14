package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

const testCommentID int64 = 21

func (f *fixture) commentDeps() CommentDeps {
	return CommentDeps{Comments: f.comments, Tasks: f.tasks, Teams: f.teams, Outbox: f.outbox, Tx: f.tx}
}

func (f *fixture) commentService(t *testing.T) *CommentService {
	t.Helper()
	svc, err := NewCommentService(f.commentDeps())
	require.NoError(t, err)
	return svc
}

func testComment() *domain.Comment {
	return &domain.Comment{
		ID:        testCommentID,
		TaskID:    testTaskID,
		UserID:    memberUser,
		Content:   "Done for staging",
		CreatedAt: testNow,
		UpdatedAt: testNow,
	}
}

func TestNewCommentService_RejectsMissingDeps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*CommentDeps)
		want   string
	}{
		{name: "comment repository", mutate: func(d *CommentDeps) { d.Comments = nil }, want: "comment repository is required"},
		{name: "task repository", mutate: func(d *CommentDeps) { d.Tasks = nil }, want: "task repository is required"},
		{name: "team repository", mutate: func(d *CommentDeps) { d.Teams = nil }, want: "team repository is required"},
		{name: "outbox", mutate: func(d *CommentDeps) { d.Outbox = nil }, want: "outbox repository is required"},
		{name: "transaction manager", mutate: func(d *CommentDeps) { d.Tx = nil }, want: "transaction manager is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deps := newFixture(t).commentDeps()
			tt.mutate(&deps)

			svc, err := NewCommentService(deps)

			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, svc)
		})
	}
}

func TestCommentService_Create(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		assignee *int64
		event    string
	}{
		{
			name:  "unassigned task",
			event: `{"task_id": 11, "team_id": 7, "comment_id": 21, "author_id": 3, "task_creator_id": 1, "task_assignee_id": null}`,
		},
		{
			name:     "assigned task",
			assignee: new(adminUser),
			event:    `{"task_id": 11, "team_id": 7, "comment_id": 21, "author_id": 3, "task_creator_id": 1, "task_assignee_id": 2}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			task := testTask()
			task.AssigneeID = tt.assignee
			stored := testComment()
			f.expectTx()
			f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(task, nil)
			f.expectRole(inTx, memberUser, domain.TeamRoleMember)
			gomock.InOrder(
				f.comments.EXPECT().Create(inTx, &domain.Comment{TaskID: testTaskID, UserID: memberUser, Content: "Done for staging"}).
					Return(testCommentID, nil),
				f.comments.EXPECT().GetByID(inTx, testCommentID).Return(stored, nil),
				f.expectEvent(t, domain.EventTaskCommented, tt.event),
			)

			comment, err := f.commentService(t).Create(t.Context(), memberUser, testTaskID, port.CreateCommentInput{Content: "  Done for staging\n"})

			require.NoError(t, err)
			assert.Same(t, stored, comment)
		})
	}
}

func TestCommentService_Create_RollsBackWhenTheEventCannotBeStored(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	boom := apperror.Internal("add outbox events", errors.New("lock wait timeout exceeded"))
	result := f.expectTx()
	f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(testTask(), nil)
	f.expectRole(inTx, memberUser, domain.TeamRoleMember)
	f.comments.EXPECT().Create(inTx, gomock.Any()).Return(testCommentID, nil)
	f.comments.EXPECT().GetByID(inTx, testCommentID).Return(testComment(), nil)
	f.outbox.EXPECT().Add(inTx, gomock.Any()).Return(boom)

	comment, err := f.commentService(t).Create(t.Context(), memberUser, testTaskID, port.CreateCommentInput{Content: "hi"})

	require.ErrorIs(t, err, boom)
	require.ErrorIs(t, result.err, boom, "the comment must be rolled back with its event")
	assert.Nil(t, comment)
}

func TestCommentService_Create_Rejections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		caller  int64
		content string
		setup   func(f *fixture)
		check   func(t *testing.T, err error)
	}{
		{
			name: "blank content", caller: memberUser, content: " \n ",
			check: func(t *testing.T, err error) { requireFieldErrors(t, err, map[string]string{"content": msgRequired}) },
		},
		{
			name: "content too long", caller: memberUser, content: strings.Repeat("ü", maxCommentChars+1),
			check: func(t *testing.T, err error) {
				requireFieldErrors(t, err, map[string]string{"content": "must be at most 5000 characters"})
			},
		},
		{
			name: "missing task", caller: memberUser, content: "hi",
			setup: func(f *fixture) {
				f.expectTx()
				f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(nil, apperror.NotFound("task", testTaskID))
			},
			check: func(t *testing.T, err error) { requireNotFound(t, err, "task", testTaskID) },
		},
		{
			name: "outsider gets the not found of a missing task", caller: outsiderUser, content: "hi",
			setup: func(f *fixture) {
				f.expectTx()
				f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(testTask(), nil)
				f.expectRole(inTx, outsiderUser, "")
			},
			check: func(t *testing.T, err error) { requireNotFound(t, err, "task", testTaskID) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}

			comment, err := f.commentService(t).Create(t.Context(), tt.caller, testTaskID, port.CreateCommentInput{Content: tt.content})

			require.Error(t, err)
			tt.check(t, err)
			assert.Nil(t, comment)
		})
	}
}

func TestCommentService_List(t *testing.T) {
	t.Parallel()

	t.Run("member", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		want := []domain.Comment{*testComment()}
		f.tasks.EXPECT().GetByID(gomock.Any(), testTaskID).Return(testTask(), nil)
		f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)
		f.comments.EXPECT().ListByTask(gomock.Any(), testTaskID).Return(want, nil)

		comments, err := f.commentService(t).List(t.Context(), memberUser, testTaskID)

		require.NoError(t, err)
		assert.Equal(t, want, comments)
	})

	t.Run("outsider", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.tasks.EXPECT().GetByID(gomock.Any(), testTaskID).Return(testTask(), nil)
		f.expectRole(gomock.Any(), outsiderUser, "")

		comments, err := f.commentService(t).List(t.Context(), outsiderUser, testTaskID)

		requireNotFound(t, err, "task", testTaskID)
		assert.Nil(t, comments)
	})
}

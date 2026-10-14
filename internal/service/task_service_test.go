package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/metrics"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/opt"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

func (f *fixture) taskDeps() TaskDeps {
	return TaskDeps{
		Tasks:    f.tasks,
		History:  f.history,
		Teams:    f.teams,
		Users:    f.users,
		Cache:    f.cache,
		Tx:       f.tx,
		Notifier: f.notifier,
		Clock:    fixedClock,
	}
}

func (f *fixture) taskService(t *testing.T) *TaskService {
	t.Helper()
	svc, err := NewTaskService(f.taskDeps())
	require.NoError(t, err)
	return svc
}

// testTask returns the stored task of the tests: open, unassigned and without a due date.
func testTask() *domain.Task {
	return &domain.Task{
		ID:          testTaskID,
		TeamID:      testTeamID,
		Title:       "Rotate the certificates",
		Description: "Before they expire",
		Status:      domain.TaskStatusTodo,
		Priority:    domain.TaskPriorityMedium,
		CreatorID:   ownerUser,
		CreatedAt:   testNow.Add(-48 * time.Hour),
		UpdatedAt:   testNow.Add(-24 * time.Hour),
	}
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// captureLog returns a context whose logger writes JSON records into the returned buffer.
func captureLog(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	log := logger.New(logger.Config{Level: "debug", Format: "json", Output: &buf})
	return logger.NewContext(t.Context(), log), &buf
}

// cacheFailures returns the operations of the logged task list cache failures, in order, after
// checking that each record is a warning about the test team.
func cacheFailures(t *testing.T, logs *bytes.Buffer) []string {
	t.Helper()
	var operations []string
	dec := json.NewDecoder(logs)
	for {
		var rec map[string]any
		err := dec.Decode(&rec)
		if errors.Is(err, io.EOF) {
			return operations
		}
		require.NoError(t, err)
		require.Equal(t, "task list cache failed", rec["msg"])
		assert.Equal(t, "WARN", rec["level"])
		assert.InDelta(t, testTeamID, rec["team_id"], 0)
		assert.NotEmpty(t, rec["error"])
		operation, ok := rec["operation"].(string)
		require.True(t, ok, "log record without an operation: %v", rec)
		operations = append(operations, operation)
	}
}

func TestNewTaskService_RejectsMissingDeps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*TaskDeps)
		want   string
	}{
		{name: "task repository", mutate: func(d *TaskDeps) { d.Tasks = nil }, want: "task repository is required"},
		{name: "history repository", mutate: func(d *TaskDeps) { d.History = nil }, want: "task history repository is required"},
		{name: "team repository", mutate: func(d *TaskDeps) { d.Teams = nil }, want: "team repository is required"},
		{name: "user repository", mutate: func(d *TaskDeps) { d.Users = nil }, want: "user repository is required"},
		{name: "cache", mutate: func(d *TaskDeps) { d.Cache = nil }, want: "task list cache is required"},
		{name: "transaction manager", mutate: func(d *TaskDeps) { d.Tx = nil }, want: "transaction manager is required"},
		{name: "notifier", mutate: func(d *TaskDeps) { d.Notifier = nil }, want: "notification service is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deps := newFixture(t).taskDeps()
			tt.mutate(&deps)

			svc, err := NewTaskService(deps)

			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, svc)
		})
	}
}

func TestTaskService_Create(t *testing.T) {
	t.Parallel()
	due := date(2026, 4, 1)
	tests := []struct {
		name     string
		in       port.CreateTaskInput
		want     domain.Task
		assignee int64
	}{
		{
			name: "defaults to medium priority, trims text",
			in:   port.CreateTaskInput{TeamID: testTeamID, Title: "  Rotate the certificates ", Description: " Before they expire\n"},
			want: domain.Task{
				TeamID: testTeamID, Title: "Rotate the certificates", Description: "Before they expire",
				Status: domain.TaskStatusTodo, Priority: domain.TaskPriorityMedium, CreatorID: memberUser,
			},
		},
		{
			name: "assigned, with priority and due date",
			in: port.CreateTaskInput{
				TeamID: testTeamID, Title: "Rotate the certificates", Priority: new(domain.TaskPriorityHigh),
				AssigneeID: new(adminUser), DueDate: &due,
			},
			want: domain.Task{
				TeamID: testTeamID, Title: "Rotate the certificates", Status: domain.TaskStatusTodo,
				Priority: domain.TaskPriorityHigh, CreatorID: memberUser, AssigneeID: new(adminUser), DueDate: &due,
			},
			assignee: adminUser,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			stored := testTask()
			stored.AssigneeID = tt.want.AssigneeID
			f.expectTx()
			f.expectRole(inTx, memberUser, domain.TeamRoleMember)
			if tt.assignee != 0 {
				f.expectRole(inTx, tt.assignee, domain.TeamRoleAdmin)
			}
			f.tasks.EXPECT().Create(inTx, &tt.want).Return(testTaskID, nil)
			f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(stored, nil)
			f.cache.EXPECT().Invalidate(afterTx, testTeamID).Return(nil)
			if tt.assignee != 0 {
				assignee := &domain.User{ID: tt.assignee, Email: "ada@example.com"}
				f.users.EXPECT().GetByID(afterTx, tt.assignee).Return(assignee, nil)
				f.notifier.EXPECT().NotifyTaskAssigned(afterTx, stored, assignee).Return(nil)
			}

			task, err := f.taskService(t).Create(t.Context(), memberUser, tt.in)

			require.NoError(t, err)
			assert.Same(t, stored, task)
		})
	}
}

func TestTaskService_Create_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   port.CreateTaskInput
		want map[string]string
	}{
		{name: "missing team and title", in: port.CreateTaskInput{Title: " "}, want: map[string]string{"team_id": msgPositive, "title": msgRequired}},
		{
			name: "title too long", in: port.CreateTaskInput{TeamID: testTeamID, Title: strings.Repeat("я", maxTaskTitleChars+1)},
			want: map[string]string{"title": "must be at most 255 characters"},
		},
		{
			name: "description too long",
			in:   port.CreateTaskInput{TeamID: testTeamID, Title: "t", Description: strings.Repeat("x", maxTaskDescriptionChars+1)},
			want: map[string]string{"description": "must be at most 10000 characters"},
		},
		{
			name: "unknown priority", in: port.CreateTaskInput{TeamID: testTeamID, Title: "t", Priority: new(domain.TaskPriority(4))},
			want: map[string]string{"priority": "must be 1 (low), 2 (medium) or 3 (high)"},
		},
		{
			name: "explicit zero priority", in: port.CreateTaskInput{TeamID: testTeamID, Title: "t", Priority: new(domain.TaskPriority(0))},
			want: map[string]string{"priority": "must be 1 (low), 2 (medium) or 3 (high)"},
		},
		{
			name: "non-positive assignee", in: port.CreateTaskInput{TeamID: testTeamID, Title: "t", AssigneeID: new(int64(0))},
			want: map[string]string{"assignee_id": msgPositive},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			task, err := newFixture(t).taskService(t).Create(t.Context(), memberUser, tt.in)

			requireFieldErrors(t, err, tt.want)
			assert.Nil(t, task)
		})
	}
}

func TestTaskService_Create_Rejections(t *testing.T) {
	t.Parallel()
	in := port.CreateTaskInput{TeamID: testTeamID, Title: "Rotate the certificates", AssigneeID: new(outsiderUser)}

	t.Run("outsider gets the not found of a missing team", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectTx()
		f.expectRole(inTx, outsiderUser, "")

		_, err := f.taskService(t).Create(t.Context(), outsiderUser, in)

		requireNotFound(t, err, "team", testTeamID)
	})

	t.Run("assignee outside the team", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectTx()
		f.expectRole(inTx, memberUser, domain.TeamRoleMember)
		f.expectRole(inTx, outsiderUser, "")

		_, err := f.taskService(t).Create(t.Context(), memberUser, in)

		requireFieldErrors(t, err, map[string]string{"assignee_id": "must be a member of the team"})
	})
}

func TestTaskService_Create_SucceedsWhenTheAssigneeCannotBeNotified(t *testing.T) {
	t.Parallel()
	assignee := &domain.User{ID: adminUser, Email: "ada@example.com"}
	tests := []struct {
		name  string
		setup func(f *fixture, stored *domain.Task)
	}{
		{
			name: "assignee lookup fails",
			setup: func(f *fixture, _ *domain.Task) {
				f.users.EXPECT().GetByID(afterTx, adminUser).Return(nil, errors.New("connection reset by peer"))
			},
		},
		{
			name: "notifier fails",
			setup: func(f *fixture, stored *domain.Task) {
				f.users.EXPECT().GetByID(afterTx, adminUser).Return(assignee, nil)
				f.notifier.EXPECT().NotifyTaskAssigned(afterTx, stored, assignee).Return(errors.New("smtp: timeout"))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			stored := testTask()
			stored.AssigneeID = new(adminUser)
			f.expectTx()
			f.expectRole(inTx, memberUser, domain.TeamRoleMember)
			f.expectRole(inTx, adminUser, domain.TeamRoleAdmin)
			f.tasks.EXPECT().Create(inTx, gomock.Any()).Return(testTaskID, nil)
			f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(stored, nil)
			f.cache.EXPECT().Invalidate(afterTx, testTeamID).Return(nil)
			tt.setup(f, stored)

			task, err := f.taskService(t).Create(t.Context(), memberUser,
				port.CreateTaskInput{TeamID: testTeamID, Title: "t", AssigneeID: new(adminUser)})

			require.NoError(t, err, "the task is committed, so a failed notification must not fail the request")
			assert.Same(t, stored, task)
		})
	}
}

func TestTaskService_Get(t *testing.T) {
	t.Parallel()

	t.Run("member", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.tasks.EXPECT().GetByID(gomock.Any(), testTaskID).Return(testTask(), nil)
		f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)

		task, err := f.taskService(t).Get(t.Context(), memberUser, testTaskID)

		require.NoError(t, err)
		assert.Equal(t, testTask(), task)
	})

	t.Run("outsider gets the not found of a missing task", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.tasks.EXPECT().GetByID(gomock.Any(), testTaskID).Return(testTask(), nil)
		f.expectRole(gomock.Any(), outsiderUser, "")

		task, err := f.taskService(t).Get(t.Context(), outsiderUser, testTaskID)

		requireNotFound(t, err, "task", testTaskID)
		assert.Nil(t, task)
	})

	t.Run("missing task", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.tasks.EXPECT().GetByID(gomock.Any(), testTaskID).Return(nil, apperror.NotFound("task", testTaskID))

		_, err := f.taskService(t).Get(t.Context(), memberUser, testTaskID)

		requireNotFound(t, err, "task", testTaskID)
	})
}

func TestTaskService_Update_RecordsEveryChangedField(t *testing.T) {
	t.Parallel()
	earlier := testNow.Add(-72 * time.Hour)
	due := date(2026, 4, 1)
	tests := []struct {
		name    string
		stored  func(*domain.Task)
		in      port.UpdateTaskInput
		want    func(*domain.Task)
		changes [][3]string // field, old value, new value
		// assignee is the new assignee whose membership must be checked; 0 means none.
		assignee int64
	}{
		{
			name:    "title, trimmed",
			in:      port.UpdateTaskInput{Title: opt.Of("  Renew the certificates ")},
			want:    func(t *domain.Task) { t.Title = "Renew the certificates" },
			changes: [][3]string{{"title", "Rotate the certificates", "Renew the certificates"}},
		},
		{
			name:    "description cleared",
			in:      port.UpdateTaskInput{Description: opt.Of("")},
			want:    func(t *domain.Task) { t.Description = "" },
			changes: [][3]string{{"description", "Before they expire", ""}},
		},
		{
			name: "status to done sets completed_at",
			in:   port.UpdateTaskInput{Status: opt.Of(domain.TaskStatusDone)},
			want: func(t *domain.Task) {
				t.Status = domain.TaskStatusDone
				t.CompletedAt = new(testNow)
			},
			changes: [][3]string{{"status", "todo", "done"}},
		},
		{
			name: "status leaving done clears completed_at",
			stored: func(t *domain.Task) {
				t.Status = domain.TaskStatusDone
				t.CompletedAt = &earlier
			},
			in: port.UpdateTaskInput{Status: opt.Of(domain.TaskStatusInProgress)},
			want: func(t *domain.Task) {
				t.Status = domain.TaskStatusInProgress
				t.CompletedAt = nil
			},
			changes: [][3]string{{"status", "done", "in_progress"}},
		},
		{
			name:    "priority",
			in:      port.UpdateTaskInput{Priority: opt.Of(domain.TaskPriorityHigh)},
			want:    func(t *domain.Task) { t.Priority = domain.TaskPriorityHigh },
			changes: [][3]string{{"priority", "2", "3"}},
		},
		{
			name:     "assignee set",
			in:       port.UpdateTaskInput{AssigneeID: opt.Of(adminUser)},
			want:     func(t *domain.Task) { t.AssigneeID = new(adminUser) },
			changes:  [][3]string{{"assignee_id", "", "2"}},
			assignee: adminUser,
		},
		{
			name:    "assignee cleared",
			stored:  func(t *domain.Task) { t.AssigneeID = new(adminUser) },
			in:      port.UpdateTaskInput{AssigneeID: opt.Null[int64]()},
			want:    func(t *domain.Task) { t.AssigneeID = nil },
			changes: [][3]string{{"assignee_id", "2", ""}},
		},
		{
			name:    "due date set",
			in:      port.UpdateTaskInput{DueDate: opt.Of(due)},
			want:    func(t *domain.Task) { t.DueDate = &due },
			changes: [][3]string{{"due_date", "", "2026-04-01"}},
		},
		{
			name:    "due date cleared",
			stored:  func(t *domain.Task) { t.DueDate = &due },
			in:      port.UpdateTaskInput{DueDate: opt.Null[time.Time]()},
			want:    func(t *domain.Task) { t.DueDate = nil },
			changes: [][3]string{{"due_date", "2026-04-01", ""}},
		},
		{
			name: "unchanged fields are left out",
			in: port.UpdateTaskInput{
				Title:      opt.Of("Rotate the certificates"),
				Status:     opt.Of(domain.TaskStatusReview),
				Priority:   opt.Of(domain.TaskPriorityMedium),
				AssigneeID: opt.Null[int64](),
			},
			want:    func(t *domain.Task) { t.Status = domain.TaskStatusReview },
			changes: [][3]string{{"status", "todo", "review"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			stored := testTask()
			if tt.stored != nil {
				tt.stored(stored)
			}
			written := *stored
			tt.want(&written)
			changes := make([]domain.TaskChange, 0, len(tt.changes))
			for _, c := range tt.changes {
				changes = append(changes, domain.TaskChange{TaskID: testTaskID, UserID: memberUser, Field: c[0], OldValue: c[1], NewValue: c[2]})
			}
			reread := &written

			f.expectTx()
			f.tasks.EXPECT().GetByIDForUpdate(inTx, testTaskID).Return(stored, nil)
			f.expectRole(inTx, memberUser, domain.TeamRoleMember)
			if tt.assignee != 0 {
				f.expectRole(inTx, tt.assignee, domain.TeamRoleAdmin)
			}
			gomock.InOrder(
				f.tasks.EXPECT().Update(inTx, &written).Return(nil),
				f.history.EXPECT().Add(inTx, changes).Return(nil),
				f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(reread, nil),
				f.cache.EXPECT().Invalidate(afterTx, testTeamID).Return(nil),
			)

			task, err := f.taskService(t).Update(t.Context(), memberUser, testTaskID, tt.in)

			require.NoError(t, err)
			assert.Same(t, reread, task)
		})
	}
}

func TestTaskService_Update_WritesNothingWhenNothingChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   port.UpdateTaskInput
	}{
		{name: "empty update"},
		{
			name: "every field set to its current value",
			in: port.UpdateTaskInput{
				Title:       opt.Of(" Rotate the certificates "),
				Description: opt.Of("Before they expire"),
				Status:      opt.Of(domain.TaskStatusTodo),
				Priority:    opt.Of(domain.TaskPriorityMedium),
				AssigneeID:  opt.Null[int64](),
				DueDate:     opt.Null[time.Time](),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			stored := testTask()
			f.expectTx()
			f.tasks.EXPECT().GetByIDForUpdate(inTx, testTaskID).Return(stored, nil)
			f.expectRole(inTx, memberUser, domain.TeamRoleMember)
			// Neither Update, history, re-read nor cache invalidation is expected.

			task, err := f.taskService(t).Update(t.Context(), memberUser, testTaskID, tt.in)

			require.NoError(t, err)
			assert.Same(t, stored, task)
		})
	}
}

func TestTaskService_Update_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   port.UpdateTaskInput
		want map[string]string
	}{
		{
			name: "null for fields that cannot be cleared",
			in: port.UpdateTaskInput{
				Title: opt.Null[string](), Description: opt.Null[string](),
				Status: opt.Null[domain.TaskStatus](), Priority: opt.Null[domain.TaskPriority](),
			},
			want: map[string]string{"title": msgNotNull, "description": msgNotNull, "status": msgNotNull, "priority": msgNotNull},
		},
		{name: "blank title", in: port.UpdateTaskInput{Title: opt.Of("  ")}, want: map[string]string{"title": msgRequired}},
		{
			name: "unknown status", in: port.UpdateTaskInput{Status: opt.Of[domain.TaskStatus]("archived")},
			want: map[string]string{"status": "must be one of todo, in_progress, review, done"},
		},
		{
			name: "unknown priority", in: port.UpdateTaskInput{Priority: opt.Of[domain.TaskPriority](0)},
			want: map[string]string{"priority": "must be 1 (low), 2 (medium) or 3 (high)"},
		},
		{name: "non-positive assignee", in: port.UpdateTaskInput{AssigneeID: opt.Of[int64](-1)}, want: map[string]string{"assignee_id": msgPositive}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			task, err := newFixture(t).taskService(t).Update(t.Context(), memberUser, testTaskID, tt.in)

			requireFieldErrors(t, err, tt.want)
			assert.Nil(t, task)
		})
	}
}

func TestTaskService_Update_Rejections(t *testing.T) {
	t.Parallel()

	t.Run("outsider gets the not found of a missing task", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectTx()
		f.tasks.EXPECT().GetByIDForUpdate(inTx, testTaskID).Return(testTask(), nil)
		f.expectRole(inTx, outsiderUser, "")

		_, err := f.taskService(t).Update(t.Context(), outsiderUser, testTaskID, port.UpdateTaskInput{Title: opt.Of("x")})

		requireNotFound(t, err, "task", testTaskID)
	})

	t.Run("missing task", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectTx()
		f.tasks.EXPECT().GetByIDForUpdate(inTx, testTaskID).Return(nil, apperror.NotFound("task", testTaskID))

		_, err := f.taskService(t).Update(t.Context(), memberUser, testTaskID, port.UpdateTaskInput{Title: opt.Of("x")})

		requireNotFound(t, err, "task", testTaskID)
	})

	t.Run("assignee outside the team", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectTx()
		f.tasks.EXPECT().GetByIDForUpdate(inTx, testTaskID).Return(testTask(), nil)
		f.expectRole(inTx, memberUser, domain.TeamRoleMember)
		f.expectRole(inTx, outsiderUser, "")

		_, err := f.taskService(t).Update(t.Context(), memberUser, testTaskID, port.UpdateTaskInput{AssigneeID: opt.Of(outsiderUser)})

		requireFieldErrors(t, err, map[string]string{"assignee_id": "must be a member of the team"})
	})
}

func TestTaskService_Update_RollsBackWhenTheHistoryCannotBeWritten(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	boom := apperror.Internal("add task history", errors.New("lock wait timeout exceeded"))
	result := f.expectTx()
	f.tasks.EXPECT().GetByIDForUpdate(inTx, testTaskID).Return(testTask(), nil)
	f.expectRole(inTx, memberUser, domain.TeamRoleMember)
	f.tasks.EXPECT().Update(inTx, gomock.Any()).Return(nil)
	f.history.EXPECT().Add(inTx, gomock.Any()).Return(boom)
	// No re-read and no cache invalidation: nothing was committed.

	task, err := f.taskService(t).Update(t.Context(), memberUser, testTaskID, port.UpdateTaskInput{Title: opt.Of("Renew")})

	require.ErrorIs(t, err, boom)
	require.ErrorIs(t, result.err, boom, "the transaction must be rolled back")
	assert.Nil(t, task)
}

// committedWrite is a write of the test task: setup expects the calls the write makes up to
// its commit, after which it invalidates the test team's cached lists.
type committedWrite struct {
	name  string
	setup func(f *fixture)
	write func(ctx context.Context, svc *TaskService) (*domain.Task, error)
}

func committedWrites() []committedWrite {
	return []committedWrite{
		{
			name: "create",
			setup: func(f *fixture) {
				f.expectRole(inTx, memberUser, domain.TeamRoleMember)
				f.tasks.EXPECT().Create(inTx, gomock.Any()).Return(testTaskID, nil)
				f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(testTask(), nil)
			},
			write: func(ctx context.Context, svc *TaskService) (*domain.Task, error) {
				return svc.Create(ctx, memberUser, port.CreateTaskInput{TeamID: testTeamID, Title: "t"})
			},
		},
		{
			name: "update",
			setup: func(f *fixture) {
				f.tasks.EXPECT().GetByIDForUpdate(inTx, testTaskID).Return(testTask(), nil)
				f.expectRole(inTx, memberUser, domain.TeamRoleMember)
				f.tasks.EXPECT().Update(inTx, gomock.Any()).Return(nil)
				f.history.EXPECT().Add(inTx, gomock.Any()).Return(nil)
				f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(testTask(), nil)
			},
			write: func(ctx context.Context, svc *TaskService) (*domain.Task, error) {
				return svc.Update(ctx, memberUser, testTaskID, port.UpdateTaskInput{Title: opt.Of("Renew")})
			},
		},
	}
}

func TestTaskService_WritesSucceedWhenTheCacheCannotBeInvalidated(t *testing.T) {
	t.Parallel()
	for _, tt := range committedWrites() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.expectTx()
			tt.setup(f)
			f.cache.EXPECT().Invalidate(afterTx, testTeamID).Return(errors.New("redis: connection refused"))
			ctx, logs := captureLog(t)

			task, err := tt.write(ctx, f.taskService(t))

			require.NoError(t, err, "the write is committed; a cache left stale until it expires must not fail it")
			assert.Equal(t, testTask(), task)
			assert.Equal(t, []string{"invalidate"}, cacheFailures(t, logs))
		})
	}
}

// TestTaskService_InvalidatesAfterTheClientHasGone cancels the request as its transaction
// commits, as a client that times out does: the committed change must still retire the
// cached lists rather than stay hidden behind them until they expire.
func TestTaskService_InvalidatesAfterTheClientHasGone(t *testing.T) {
	t.Parallel()
	for _, tt := range committedWrites() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			logCtx, logs := captureLog(t)
			reqCtx, disconnect := context.WithCancel(logCtx)
			f.tx.EXPECT().WithinTx(afterTx, gomock.Any()).DoAndReturn(
				func(ctx context.Context, fn func(context.Context) error) error {
					defer disconnect()
					return fn(context.WithValue(ctx, txMarker{}, true))
				})
			tt.setup(f)
			f.cache.EXPECT().Invalidate(afterTx, testTeamID).DoAndReturn(
				func(ctx context.Context, _ int64) error { return ctx.Err() })

			_, err := tt.write(reqCtx, f.taskService(t))

			require.NoError(t, err)
			assert.Empty(t, cacheFailures(t, logs), "the invalidation must ignore the canceled request")
		})
	}
}

func TestTaskService_List_AcrossTeamsIsScopedToTheViewer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	tasks := []domain.Task{*testTask()}
	// The repository receives the viewer, which it scopes the query by. The cache mock expects
	// no call: its keys do not include the viewer, so lists across teams must bypass it.
	f.tasks.EXPECT().List(gomock.Any(), domain.TaskQuery{ViewerID: memberUser, Status: domain.TaskStatusTodo, Page: 1, PageSize: 20}).
		Return(tasks, 41, nil)

	page, err := f.taskService(t).List(t.Context(), domain.TaskQuery{ViewerID: memberUser, Status: domain.TaskStatusTodo})

	require.NoError(t, err)
	assert.Equal(t, &domain.TaskPage{Tasks: tasks, Total: 41, Page: 1, PageSize: 20, TotalPages: 3}, page)
}

func TestTaskService_List_OfOneTeamUsesTheCache(t *testing.T) {
	t.Parallel()
	const ver int64 = 42
	in := domain.TaskQuery{ViewerID: memberUser, TeamID: testTeamID, Status: domain.TaskStatusTodo, AssigneeID: adminUser}
	// The cache is keyed by the normalized query.
	q := in
	q.Page, q.PageSize = 1, 20
	stored := &domain.TaskPage{Tasks: []domain.Task{*testTask()}, Total: 21, Page: 1, PageSize: 20, TotalPages: 2}
	redisDown := errors.New("redis: i/o timeout")
	tests := []struct {
		name         string
		setup        func(f *fixture)
		wantFailures []string // cache operations whose failure is logged
	}{
		{
			name: "hit",
			setup: func(f *fixture) {
				gomock.InOrder(
					f.cache.EXPECT().Version(gomock.Any(), testTeamID).Return(ver, nil),
					f.cache.EXPECT().Get(gomock.Any(), testTeamID, ver, q).Return(stored, true, nil),
				)
			},
		},
		{
			name: "miss is filled under the version read before the query",
			setup: func(f *fixture) {
				gomock.InOrder(
					f.cache.EXPECT().Version(gomock.Any(), testTeamID).Return(ver, nil),
					f.cache.EXPECT().Get(gomock.Any(), testTeamID, ver, q).Return(nil, false, nil),
					f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 21, nil),
					f.cache.EXPECT().Set(gomock.Any(), testTeamID, ver, q, stored).Return(nil),
				)
			},
		},
		{
			name: "unreadable page falls back to the database and is replaced",
			setup: func(f *fixture) {
				gomock.InOrder(
					f.cache.EXPECT().Version(gomock.Any(), testTeamID).Return(ver, nil),
					f.cache.EXPECT().Get(gomock.Any(), testTeamID, ver, q).Return(nil, false, redisDown),
					f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 21, nil),
					f.cache.EXPECT().Set(gomock.Any(), testTeamID, ver, q, stored).Return(nil),
				)
			},
			wantFailures: []string{"get"},
		},
		{
			name: "failed write still answers from the database",
			setup: func(f *fixture) {
				gomock.InOrder(
					f.cache.EXPECT().Version(gomock.Any(), testTeamID).Return(ver, nil),
					f.cache.EXPECT().Get(gomock.Any(), testTeamID, ver, q).Return(nil, false, nil),
					f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 21, nil),
					f.cache.EXPECT().Set(gomock.Any(), testTeamID, ver, q, stored).Return(redisDown),
				)
			},
			wantFailures: []string{"set"},
		},
		{
			name: "unreadable version bypasses the cache",
			setup: func(f *fixture) {
				gomock.InOrder(
					f.cache.EXPECT().Version(gomock.Any(), testTeamID).Return(int64(0), redisDown),
					f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 21, nil),
				)
			},
			wantFailures: []string{"version"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)
			tt.setup(f)
			ctx, logs := captureLog(t)

			page, err := f.taskService(t).List(ctx, in)

			require.NoError(t, err)
			assert.Equal(t, stored, page)
			assert.Equal(t, tt.wantFailures, cacheFailures(t, logs))
		})
	}
}

// TestTaskService_List_NeverServesAPageReadBeforeAConcurrentWrite lets a write commit while a
// reader queries the database. The cache mock keeps pages by version, as the Redis keys do.
func TestTaskService_List_NeverServesAPageReadBeforeAConcurrentWrite(t *testing.T) {
	t.Parallel()
	q := domain.TaskQuery{ViewerID: memberUser, TeamID: testTeamID, Page: 1, PageSize: 20}
	beforeWrite, afterWrite := *testTask(), *testTask()
	afterWrite.Status = domain.TaskStatusDone
	const readers = 3
	f := newFixture(t)
	version := int64(4)
	pages := map[int64]*domain.TaskPage{}
	f.teams.EXPECT().GetMember(gomock.Any(), testTeamID, memberUser).
		Return(&domain.TeamMember{TeamID: testTeamID, UserID: memberUser, Role: domain.TeamRoleMember}, nil).Times(readers)
	f.cache.EXPECT().Version(gomock.Any(), testTeamID).
		DoAndReturn(func(context.Context, int64) (int64, error) { return version, nil }).Times(readers)
	f.cache.EXPECT().Get(gomock.Any(), testTeamID, gomock.Any(), q).
		DoAndReturn(func(_ context.Context, _, ver int64, _ domain.TaskQuery) (*domain.TaskPage, bool, error) {
			page, ok := pages[ver]
			return page, ok, nil
		}).Times(readers)
	f.cache.EXPECT().Set(gomock.Any(), testTeamID, gomock.Any(), q, gomock.Any()).
		DoAndReturn(func(_ context.Context, _, ver int64, _ domain.TaskQuery, page *domain.TaskPage) error {
			pages[ver] = page
			return nil
		}).Times(2)
	gomock.InOrder(
		f.tasks.EXPECT().List(gomock.Any(), q).DoAndReturn(func(context.Context, domain.TaskQuery) ([]domain.Task, int, error) {
			version++ // the write commits and invalidates while this query runs
			return []domain.Task{beforeWrite}, 1, nil
		}),
		f.tasks.EXPECT().List(gomock.Any(), q).Return([]domain.Task{afterWrite}, 1, nil),
	)
	svc := f.taskService(t)

	racing, err := svc.List(t.Context(), q)
	require.NoError(t, err)
	next, err := svc.List(t.Context(), q)
	require.NoError(t, err)
	cached, err := svc.List(t.Context(), q)
	require.NoError(t, err)

	assert.Equal(t, []domain.Task{beforeWrite}, racing.Tasks, "the racing reader answers with what it read")
	assert.Equal(t, racing, pages[4], "but caches it under the version the write retired")
	assert.Equal(t, []domain.Task{afterWrite}, next.Tasks, "the next reader misses and sees the write")
	assert.Same(t, next, cached, "and its page is served from then on")
}

// TestTaskService_CountsCacheRequests does not run in parallel: the counter is process-wide,
// and the parallel tests that use the cache would skew the deltas asserted here.
func TestTaskService_CountsCacheRequests(t *testing.T) {
	const ver int64 = 42
	q := domain.TaskQuery{ViewerID: memberUser, TeamID: testTeamID, Page: 1, PageSize: 20}
	stored := &domain.TaskPage{Tasks: []domain.Task{*testTask()}, Total: 1, Page: 1, PageSize: 20, TotalPages: 1}
	redisDown := errors.New("redis: i/o timeout")
	list := func(ctx context.Context, svc *TaskService) error {
		_, err := svc.List(ctx, q)
		return err
	}
	// listing expects the calls a list of the test team makes up to reading the version.
	listing := func(f *fixture, verErr error) {
		f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)
		f.cache.EXPECT().Version(gomock.Any(), testTeamID).Return(ver, verErr)
	}
	tests := []struct {
		name  string
		setup func(f *fixture)
		call  func(ctx context.Context, svc *TaskService) error
		want  map[string]float64 // increments by result
	}{
		{
			name: "hit",
			setup: func(f *fixture) {
				listing(f, nil)
				f.cache.EXPECT().Get(gomock.Any(), testTeamID, ver, q).Return(stored, true, nil)
			},
			call: list,
			want: map[string]float64{metrics.ResultHit: 1},
		},
		{
			name: "miss",
			setup: func(f *fixture) {
				listing(f, nil)
				f.cache.EXPECT().Get(gomock.Any(), testTeamID, ver, q).Return(nil, false, nil)
				f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 1, nil)
				f.cache.EXPECT().Set(gomock.Any(), testTeamID, ver, q, stored).Return(nil)
			},
			call: list,
			want: map[string]float64{metrics.ResultMiss: 1},
		},
		{
			name: "unreadable page",
			setup: func(f *fixture) {
				listing(f, nil)
				f.cache.EXPECT().Get(gomock.Any(), testTeamID, ver, q).Return(nil, false, redisDown)
				f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 1, nil)
				f.cache.EXPECT().Set(gomock.Any(), testTeamID, ver, q, stored).Return(nil)
			},
			call: list,
			want: map[string]float64{metrics.ResultError: 1},
		},
		{
			name: "failed write",
			setup: func(f *fixture) {
				listing(f, nil)
				f.cache.EXPECT().Get(gomock.Any(), testTeamID, ver, q).Return(nil, false, nil)
				f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 1, nil)
				f.cache.EXPECT().Set(gomock.Any(), testTeamID, ver, q, stored).Return(redisDown)
			},
			call: list,
			want: map[string]float64{metrics.ResultMiss: 1, metrics.ResultError: 1},
		},
		{
			name: "unreadable version",
			setup: func(f *fixture) {
				listing(f, redisDown)
				f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 1, nil)
			},
			call: list,
			want: map[string]float64{metrics.ResultError: 1},
		},
		{
			name: "failed invalidation",
			setup: func(f *fixture) {
				f.expectTx()
				f.expectRole(inTx, memberUser, domain.TeamRoleMember)
				f.tasks.EXPECT().Create(inTx, gomock.Any()).Return(testTaskID, nil)
				f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(testTask(), nil)
				f.cache.EXPECT().Invalidate(afterTx, testTeamID).Return(redisDown)
			},
			call: func(ctx context.Context, svc *TaskService) error {
				_, err := svc.Create(ctx, memberUser, port.CreateTaskInput{TeamID: testTeamID, Title: "t"})
				return err
			},
			want: map[string]float64{metrics.ResultError: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			ctx, _ := captureLog(t) // keeps the logged failures out of the test output
			want := cacheRequestCounts()
			for result, n := range tt.want {
				want[result] += n
			}

			require.NoError(t, tt.call(ctx, f.taskService(t)))

			assert.Equal(t, want, cacheRequestCounts(), "exactly the expected series are incremented")
		})
	}
}

// cacheRequestCounts returns every series of the task list cache counter keyed by result.
func cacheRequestCounts() map[string]float64 {
	counts := make(map[string]float64)
	for _, result := range []string{metrics.ResultHit, metrics.ResultMiss, metrics.ResultError} {
		counts[result] = testutil.ToFloat64(metrics.CacheRequests.WithLabelValues(metrics.CacheTaskList, result))
	}
	return counts
}

func TestTaskService_List_PropagatesRepositoryErrors(t *testing.T) {
	t.Parallel()
	boom := apperror.Internal("list tasks", errors.New("lock wait timeout exceeded"))
	tests := []struct {
		name  string
		q     domain.TaskQuery
		setup func(f *fixture)
	}{
		{name: "across teams", q: domain.TaskQuery{ViewerID: memberUser}},
		{
			name: "of one team",
			q:    domain.TaskQuery{ViewerID: memberUser, TeamID: testTeamID},
			setup: func(f *fixture) {
				f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)
				f.cache.EXPECT().Version(gomock.Any(), testTeamID).Return(int64(0), nil)
				f.cache.EXPECT().Get(gomock.Any(), testTeamID, int64(0), gomock.Any()).Return(nil, false, nil)
				// No Set: a failed query must not be cached.
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			f.tasks.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, 0, boom)

			page, err := f.taskService(t).List(t.Context(), tt.q)

			require.ErrorIs(t, err, boom)
			assert.Nil(t, page)
		})
	}
}

func TestTaskService_List_Rejections(t *testing.T) {
	t.Parallel()

	t.Run("outsider of the requested team", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.expectRole(gomock.Any(), outsiderUser, "")

		_, err := f.taskService(t).List(t.Context(), domain.TaskQuery{ViewerID: outsiderUser, TeamID: testTeamID})

		requireNotFound(t, err, "team", testTeamID)
	})

	tests := []struct {
		name string
		q    domain.TaskQuery
		want map[string]string
	}{
		{name: "unknown status", q: domain.TaskQuery{Status: "Done"}, want: map[string]string{"status": "must be one of todo, in_progress, review, done"}},
		{name: "page size above the maximum", q: domain.TaskQuery{PageSize: 101}, want: map[string]string{"page_size": "must be between 1 and 100"}},
		{name: "page above the maximum", q: domain.TaskQuery{Page: 922337203685477581}, want: map[string]string{"page": "must be between 1 and 10000"}},
		{
			name: "negative numbers",
			q:    domain.TaskQuery{TeamID: -1, AssigneeID: -2, Page: -3, PageSize: -4},
			want: map[string]string{
				"team_id": msgPositive, "assignee_id": msgPositive,
				"page": "must be between 1 and 10000", "page_size": "must be between 1 and 100",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.q.ViewerID = memberUser

			page, err := newFixture(t).taskService(t).List(t.Context(), tt.q)

			requireFieldErrors(t, err, tt.want)
			assert.Nil(t, page)
		})
	}
}

func TestTaskService_History(t *testing.T) {
	t.Parallel()

	t.Run("member", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		want := []domain.TaskChange{{ID: 3, TaskID: testTaskID, UserID: adminUser, Field: "status", OldValue: "todo", NewValue: "done"}}
		f.tasks.EXPECT().GetByID(gomock.Any(), testTaskID).Return(testTask(), nil)
		f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)
		f.history.EXPECT().ListByTask(gomock.Any(), testTaskID).Return(want, nil)

		changes, err := f.taskService(t).History(t.Context(), memberUser, testTaskID)

		require.NoError(t, err)
		assert.Equal(t, want, changes)
	})

	t.Run("outsider", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.tasks.EXPECT().GetByID(gomock.Any(), testTaskID).Return(testTask(), nil)
		f.expectRole(gomock.Any(), outsiderUser, "")

		_, err := f.taskService(t).History(t.Context(), outsiderUser, testTaskID)

		requireNotFound(t, err, "task", testTaskID)
	})
}

func TestTaskService_ListOrphaned(t *testing.T) {
	t.Parallel()
	orphaned := []domain.OrphanedAssignment{{TaskID: testTaskID, TaskTitle: "Rotate the certificates", AssigneeID: 9, AssigneeName: "Former Member"}}
	tests := []struct {
		name     string
		caller   int64
		role     domain.TeamRole
		wantCode string
	}{
		{name: "owner", caller: ownerUser, role: domain.TeamRoleOwner},
		{name: "admin", caller: adminUser, role: domain.TeamRoleAdmin},
		{name: "member", caller: memberUser, role: domain.TeamRoleMember, wantCode: apperror.CodeForbidden},
		{name: "outsider", caller: outsiderUser, wantCode: apperror.CodeNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.expectRole(gomock.Any(), tt.caller, tt.role)
			if tt.wantCode == "" {
				f.tasks.EXPECT().ListOrphaned(gomock.Any(), testTeamID).Return(orphaned, nil)
			}

			got, err := f.taskService(t).ListOrphaned(t.Context(), tt.caller, testTeamID)

			if tt.wantCode != "" {
				require.Error(t, err)
				assertAppError(t, err, tt.wantCode)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, orphaned, got)
		})
	}
}

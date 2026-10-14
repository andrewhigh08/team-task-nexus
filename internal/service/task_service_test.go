package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
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
		{name: "cache", mutate: func(d *TaskDeps) { d.Cache = nil }, want: "task cache is required"},
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
			f.cache.EXPECT().InvalidateTeam(afterTx, testTeamID).Return(nil)
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

func TestTaskService_Create_SucceedsWhenTheCacheCannotBeInvalidated(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.expectTx()
	f.expectRole(inTx, memberUser, domain.TeamRoleMember)
	f.tasks.EXPECT().Create(inTx, gomock.Any()).Return(testTaskID, nil)
	f.tasks.EXPECT().GetByID(inTx, testTaskID).Return(testTask(), nil)
	f.cache.EXPECT().InvalidateTeam(afterTx, testTeamID).Return(errors.New("redis: connection refused"))

	task, err := f.taskService(t).Create(t.Context(), memberUser, port.CreateTaskInput{TeamID: testTeamID, Title: "t"})

	require.NoError(t, err)
	assert.Equal(t, testTask(), task)
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
			f.cache.EXPECT().InvalidateTeam(afterTx, testTeamID).Return(nil)
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
				f.cache.EXPECT().InvalidateTeam(afterTx, testTeamID).Return(nil),
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

func TestTaskService_List_AcrossTeamsIsScopedToTheViewer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	tasks := []domain.Task{*testTask()}
	// The repository receives the viewer, which it scopes the query by; the cache is never
	// consulted, since its keys do not include the viewer.
	f.tasks.EXPECT().List(gomock.Any(), domain.TaskQuery{ViewerID: memberUser, Status: domain.TaskStatusTodo, Page: 1, PageSize: 20}).
		Return(tasks, 41, nil)

	page, err := f.taskService(t).List(t.Context(), domain.TaskQuery{ViewerID: memberUser, Status: domain.TaskStatusTodo})

	require.NoError(t, err)
	assert.Equal(t, &domain.TaskPage{Tasks: tasks, Total: 41, Page: 1, PageSize: 20, TotalPages: 3}, page)
}

func TestTaskService_List_OfOneTeamUsesTheCache(t *testing.T) {
	t.Parallel()
	q := domain.TaskQuery{ViewerID: memberUser, TeamID: testTeamID, AssigneeID: adminUser, Page: 2, PageSize: 5}
	stored := &domain.TaskPage{Tasks: []domain.Task{*testTask()}, Total: 6, Page: 2, PageSize: 5, TotalPages: 2}
	tests := []struct {
		name  string
		setup func(f *fixture)
	}{
		{
			name: "hit",
			setup: func(f *fixture) {
				f.cache.EXPECT().GetTaskList(gomock.Any(), q).Return(stored, nil)
			},
		},
		{
			name: "miss",
			setup: func(f *fixture) {
				f.cache.EXPECT().GetTaskList(gomock.Any(), q).Return(nil, nil)
				f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 6, nil)
				f.cache.EXPECT().SetTaskList(gomock.Any(), q, stored).Return(nil)
			},
		},
		{
			name: "unreadable cache falls back to the database",
			setup: func(f *fixture) {
				f.cache.EXPECT().GetTaskList(gomock.Any(), q).Return(nil, errors.New("redis: i/o timeout"))
				f.tasks.EXPECT().List(gomock.Any(), q).Return(stored.Tasks, 6, nil)
				f.cache.EXPECT().SetTaskList(gomock.Any(), q, stored).Return(errors.New("redis: i/o timeout"))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.expectRole(gomock.Any(), memberUser, domain.TeamRoleMember)
			tt.setup(f)

			page, err := f.taskService(t).List(t.Context(), q)

			require.NoError(t, err)
			assert.Equal(t, stored, page)
		})
	}
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
				f.cache.EXPECT().GetTaskList(gomock.Any(), gomock.Any()).Return(nil, nil)
				// No SetTaskList: a failed query must not be cached.
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

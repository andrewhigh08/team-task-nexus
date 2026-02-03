package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newTaskServiceDeps() (
	*mocks.TaskRepositoryMock,
	*mocks.TeamRepositoryMock,
	*mocks.UserRepositoryMock,
	*mocks.TaskHistoryRepositoryMock,
	*mocks.TaskCacheMock,
	*mocks.TransactionManagerMock,
	*mocks.NotificationServiceMock,
) {
	return new(mocks.TaskRepositoryMock),
		new(mocks.TeamRepositoryMock),
		new(mocks.UserRepositoryMock),
		new(mocks.TaskHistoryRepositoryMock),
		new(mocks.TaskCacheMock),
		new(mocks.TransactionManagerMock),
		new(mocks.NotificationServiceMock)
}

func TestTaskService_Create_Success(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	taskRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.Task")).Return(int64(1), nil)
	cache.On("InvalidateTeam", mock.Anything, int64(1)).Return(nil)
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Task{
		ID: 1, Title: "Test Task", TeamID: 1, Status: domain.TaskStatusTodo,
	}, nil)

	result, err := svc.Create(context.Background(), 1, domain.CreateTaskRequest{
		Title:  "Test Task",
		TeamID: 1,
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Test Task", result.Title)
	taskRepo.AssertExpectations(t)
}

func TestTaskService_Create_EmptyTitle(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	result, err := svc.Create(context.Background(), 1, domain.CreateTaskRequest{
		Title:  "",
		TeamID: 1,
	})

	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestTaskService_Create_NotTeamMember(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(99)).Return(nil, nil)

	result, err := svc.Create(context.Background(), 99, domain.CreateTaskRequest{
		Title:  "Test Task",
		TeamID: 1,
	})

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Equal(t, apperror.ErrNotTeamMember, err)
}

func TestTaskService_Create_WithAssignee(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	assigneeID := int64(2)
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	taskRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.Task")).Return(int64(1), nil)
	cache.On("InvalidateTeam", mock.Anything, int64(1)).Return(nil)
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Task{
		ID: 1, Title: "Test Task", TeamID: 1,
		AssigneeID: sql.NullInt64{Int64: 2, Valid: true},
	}, nil)
	userRepo.On("GetByID", mock.Anything, int64(2)).Return(&domain.User{
		ID: 2, Email: "assignee@example.com", FullName: "Assignee",
	}, nil)
	notifSvc.On("NotifyTaskAssigned", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	result, err := svc.Create(context.Background(), 1, domain.CreateTaskRequest{
		Title:      "Test Task",
		TeamID:     1,
		AssigneeID: &assigneeID,
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
	notifSvc.AssertExpectations(t)
}

func TestTaskService_Update_Success(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	existingTask := &domain.Task{
		ID: 1, Title: "Old Title", Status: domain.TaskStatusTodo, TeamID: 1,
	}
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(existingTask, nil).Once()
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	txManager.On("WithTransaction", mock.Anything, mock.AnythingOfType("func(context.Context) error")).Return(nil)
	historyRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.TaskHistory")).Return(nil)
	taskRepo.On("Update", mock.Anything, mock.AnythingOfType("*domain.Task")).Return(nil)
	cache.On("InvalidateTeam", mock.Anything, int64(1)).Return(nil)

	updatedTask := &domain.Task{
		ID: 1, Title: "New Title", Status: domain.TaskStatusTodo, TeamID: 1,
	}
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(updatedTask, nil).Once()

	newTitle := "New Title"
	result, err := svc.Update(context.Background(), 1, 1, domain.UpdateTaskRequest{
		Title: &newTitle,
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "New Title", result.Title)
}

func TestTaskService_List_WithCache(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	filter := domain.TaskFilter{TeamID: 1, Page: 1, PageSize: 20}
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleMember,
	}, nil)

	cachedResponse := &domain.TaskListResponse{
		Tasks:      []domain.Task{{ID: 1, Title: "Cached Task"}},
		Total:      1,
		Page:       1,
		PageSize:   20,
		TotalPages: 1,
	}
	cache.On("GetTaskList", mock.Anything, filter).Return(cachedResponse, nil)

	result, err := svc.List(context.Background(), 1, filter)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Tasks, 1)
	assert.Equal(t, "Cached Task", result.Tasks[0].Title)
	taskRepo.AssertNotCalled(t, "List")
}

func TestTaskService_List_CacheMiss(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	filter := domain.TaskFilter{TeamID: 1, Page: 1, PageSize: 20}
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleMember,
	}, nil)
	cache.On("GetTaskList", mock.Anything, filter).Return(nil, nil)
	taskRepo.On("List", mock.Anything, filter).Return([]domain.Task{
		{ID: 1, Title: "DB Task"},
	}, 1, nil)
	cache.On("SetTaskList", mock.Anything, filter, mock.AnythingOfType("*domain.TaskListResponse")).Return(nil)

	result, err := svc.List(context.Background(), 1, filter)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Tasks, 1)
	assert.Equal(t, "DB Task", result.Tasks[0].Title)
	taskRepo.AssertExpectations(t)
}

func TestTaskService_GetHistory_Success(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Task{
		ID: 1, TeamID: 1,
	}, nil)
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleMember,
	}, nil)
	historyRepo.On("ListByTaskID", mock.Anything, int64(1)).Return([]domain.TaskHistory{
		{TaskID: 1, Field: "status", OldValue: "todo", NewValue: "in_progress"},
	}, nil)

	result, err := svc.GetHistory(context.Background(), 1, 1)

	assert.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestTaskService_GetHistory_NotMember(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Task{
		ID: 1, TeamID: 1,
	}, nil)
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(99)).Return(nil, nil)

	result, err := svc.GetHistory(context.Background(), 99, 1)

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Equal(t, apperror.ErrNotTeamMember, err)
}

func TestTaskService_Update_AllFields(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	existingTask := &domain.Task{
		ID: 1, Title: "Old Title", Description: "Old Desc",
		Status: domain.TaskStatusTodo, Priority: domain.TaskPriorityLow,
		TeamID: 1, AssigneeID: sql.NullInt64{Int64: 1, Valid: true},
		DueDate: sql.NullTime{Valid: false},
	}
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(existingTask, nil).Once()
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	txManager.On("WithTransaction", mock.Anything, mock.AnythingOfType("func(context.Context) error")).Return(nil)
	historyRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.TaskHistory")).Return(nil)
	taskRepo.On("Update", mock.Anything, mock.AnythingOfType("*domain.Task")).Return(nil)
	cache.On("InvalidateTeam", mock.Anything, int64(1)).Return(nil)

	updatedTask := &domain.Task{
		ID: 1, Title: "New Title", Description: "New Desc",
		Status: domain.TaskStatusInProgress, Priority: domain.TaskPriorityHigh,
		TeamID: 1, AssigneeID: sql.NullInt64{Int64: 2, Valid: true},
	}
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(updatedTask, nil).Once()

	newTitle := "New Title"
	newDesc := "New Desc"
	newStatus := "in_progress"
	newPriority := 3
	newAssignee := int64(2)
	newDue := "2026-12-31"

	result, err := svc.Update(context.Background(), 1, 1, domain.UpdateTaskRequest{
		Title:       &newTitle,
		Description: &newDesc,
		Status:      &newStatus,
		Priority:    &newPriority,
		AssigneeID:  &newAssignee,
		DueDate:     &newDue,
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "New Title", result.Title)
}

func TestTaskService_Update_NotMember(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Task{
		ID: 1, TeamID: 1,
	}, nil)
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(99)).Return(nil, nil)

	newTitle := "X"
	result, err := svc.Update(context.Background(), 99, 1, domain.UpdateTaskRequest{
		Title: &newTitle,
	})

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Equal(t, apperror.ErrNotTeamMember, err)
}

func TestTaskService_Update_TaskNotFound(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	taskRepo.On("GetByID", mock.Anything, int64(999)).Return(nil, apperror.NotFound("task not found"))

	newTitle := "X"
	result, err := svc.Update(context.Background(), 1, 999, domain.UpdateTaskRequest{
		Title: &newTitle,
	})

	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestTaskService_Create_WithDueDate(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	taskRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.Task")).Return(int64(1), nil)
	cache.On("InvalidateTeam", mock.Anything, int64(1)).Return(nil)
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(&domain.Task{
		ID: 1, Title: "Test Task", TeamID: 1,
	}, nil)

	result, err := svc.Create(context.Background(), 1, domain.CreateTaskRequest{
		Title:   "Test Task",
		TeamID:  1,
		DueDate: "2026-12-31",
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestTaskService_Create_InvalidDueDate(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)

	result, err := svc.Create(context.Background(), 1, domain.CreateTaskRequest{
		Title:   "Test Task",
		TeamID:  1,
		DueDate: "not-a-date",
	})

	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestTaskService_Create_NoTeamID(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	result, err := svc.Create(context.Background(), 1, domain.CreateTaskRequest{
		Title:  "Test Task",
		TeamID: 0,
	})

	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestTaskService_List_NoTeamFilter(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	filter := domain.TaskFilter{Page: 1, PageSize: 20}
	cache.On("GetTaskList", mock.Anything, filter).Return(nil, nil)
	taskRepo.On("List", mock.Anything, filter).Return([]domain.Task{}, 0, nil)
	cache.On("SetTaskList", mock.Anything, filter, mock.AnythingOfType("*domain.TaskListResponse")).Return(nil)

	result, err := svc.List(context.Background(), 1, filter)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Tasks)
}

func TestTaskService_Update_DueDateWithExistingDueDate(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	existingTask := &domain.Task{
		ID: 1, Title: "Task", Status: domain.TaskStatusTodo, TeamID: 1,
		Priority:    domain.TaskPriorityMedium,
		Description: "desc",
		DueDate:     sql.NullTime{Valid: true, Time: func() time.Time { t, _ := time.Parse("2006-01-02", "2026-01-01"); return t }()},
	}
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(existingTask, nil).Once()
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	txManager.On("WithTransaction", mock.Anything, mock.AnythingOfType("func(context.Context) error")).Return(nil)
	historyRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.TaskHistory")).Return(nil)
	taskRepo.On("Update", mock.Anything, mock.AnythingOfType("*domain.Task")).Return(nil)
	cache.On("InvalidateTeam", mock.Anything, int64(1)).Return(nil)
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(existingTask, nil).Once()

	newDue := "2026-06-15"
	result, err := svc.Update(context.Background(), 1, 1, domain.UpdateTaskRequest{
		DueDate: &newDue,
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestTaskService_Update_UnassignedToAssigned(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	existingTask := &domain.Task{
		ID: 1, Title: "Task", Status: domain.TaskStatusTodo, TeamID: 1,
		Priority: domain.TaskPriorityMedium,
	}
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(existingTask, nil).Once()
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	txManager.On("WithTransaction", mock.Anything, mock.AnythingOfType("func(context.Context) error")).Return(nil)
	historyRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.TaskHistory")).Return(nil)
	taskRepo.On("Update", mock.Anything, mock.AnythingOfType("*domain.Task")).Return(nil)
	cache.On("InvalidateTeam", mock.Anything, int64(1)).Return(nil)
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(existingTask, nil).Once()

	newAssignee := int64(5)
	result, err := svc.Update(context.Background(), 1, 1, domain.UpdateTaskRequest{
		AssigneeID: &newAssignee,
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestTaskService_Update_InvalidDueDate(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	existingTask := &domain.Task{
		ID: 1, Title: "Task", Status: domain.TaskStatusTodo, TeamID: 1,
	}
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(existingTask, nil).Once()
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	txManager.On("WithTransaction", mock.Anything, mock.AnythingOfType("func(context.Context) error")).Return(nil)

	badDate := "not-a-date"
	result, err := svc.Update(context.Background(), 1, 1, domain.UpdateTaskRequest{
		DueDate: &badDate,
	})

	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestTaskService_Update_StatusChange(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	existingTask := &domain.Task{
		ID: 1, Title: "Task", Status: domain.TaskStatusTodo, TeamID: 1,
		Priority: domain.TaskPriorityMedium,
	}
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(existingTask, nil).Once()
	teamRepo.On("GetMember", mock.Anything, int64(1), int64(1)).Return(&domain.TeamMember{
		TeamID: 1, UserID: 1, Role: domain.TeamRoleOwner,
	}, nil)
	txManager.On("WithTransaction", mock.Anything, mock.AnythingOfType("func(context.Context) error")).Return(nil)
	historyRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.TaskHistory")).Return(nil)
	taskRepo.On("Update", mock.Anything, mock.AnythingOfType("*domain.Task")).Return(nil)
	cache.On("InvalidateTeam", mock.Anything, int64(1)).Return(nil)

	updatedTask := &domain.Task{
		ID: 1, Title: "Task", Status: domain.TaskStatusDone, TeamID: 1,
	}
	taskRepo.On("GetByID", mock.Anything, int64(1)).Return(updatedTask, nil).Once()

	newStatus := "done"
	result, err := svc.Update(context.Background(), 1, 1, domain.UpdateTaskRequest{
		Status: &newStatus,
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestTaskService_GetOrphanedAssignees(t *testing.T) {
	taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc := newTaskServiceDeps()
	svc := NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, cache, txManager, notifSvc)

	expected := []domain.OrphanedAssignee{
		{TaskID: 1, TaskTitle: "Task 1", AssigneeID: 5, AssigneeName: "Ghost User"},
	}
	taskRepo.On("GetOrphanedAssignees", mock.Anything).Return(expected, nil)

	result, err := svc.GetOrphanedAssignees(context.Background())

	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "Ghost User", result[0].AssigneeName)
}

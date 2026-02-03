package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/internal/port"
)

type TaskServiceImpl struct {
	taskRepo    port.TaskRepository
	teamRepo    port.TeamRepository
	userRepo    port.UserRepository
	historyRepo port.TaskHistoryRepository
	taskCache   port.TaskCache
	txManager   port.TransactionManager
	notifSvc    port.NotificationService
}

func NewTaskService(
	taskRepo port.TaskRepository,
	teamRepo port.TeamRepository,
	userRepo port.UserRepository,
	historyRepo port.TaskHistoryRepository,
	taskCache port.TaskCache,
	txManager port.TransactionManager,
	notifSvc port.NotificationService,
) *TaskServiceImpl {
	return &TaskServiceImpl{
		taskRepo:    taskRepo,
		teamRepo:    teamRepo,
		userRepo:    userRepo,
		historyRepo: historyRepo,
		taskCache:   taskCache,
		txManager:   txManager,
		notifSvc:    notifSvc,
	}
}

func (s *TaskServiceImpl) Create(ctx context.Context, userID int64, req domain.CreateTaskRequest) (*domain.Task, error) {
	if req.Title == "" {
		return nil, apperror.BadRequest("task title is required")
	}
	if req.TeamID == 0 {
		return nil, apperror.BadRequest("team_id is required")
	}

	member, err := s.teamRepo.GetMember(ctx, req.TeamID, userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, apperror.ErrNotTeamMember
	}

	task := &domain.Task{
		Title:       req.Title,
		Description: req.Description,
		Status:      domain.TaskStatusTodo,
		Priority:    domain.TaskPriority(req.Priority),
		TeamID:      req.TeamID,
		CreatorID:   userID,
	}
	if task.Priority == 0 {
		task.Priority = domain.TaskPriorityMedium
	}

	if req.AssigneeID != nil {
		task.AssigneeID = sql.NullInt64{Int64: *req.AssigneeID, Valid: true}
	}
	if req.DueDate != "" {
		t, err := time.Parse("2006-01-02", req.DueDate)
		if err != nil {
			return nil, apperror.BadRequest("invalid due_date format, use YYYY-MM-DD")
		}
		task.DueDate = sql.NullTime{Time: t, Valid: true}
	}

	id, err := s.taskRepo.Create(ctx, task)
	if err != nil {
		return nil, err
	}

	_ = s.taskCache.InvalidateTeam(ctx, req.TeamID)

	task, err = s.taskRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if task.AssigneeID.Valid {
		assignee, aErr := s.userRepo.GetByID(ctx, task.AssigneeID.Int64)
		if aErr == nil {
			_ = s.notifSvc.NotifyTaskAssigned(ctx, task, assignee)
		}
	}

	return task, nil
}

func (s *TaskServiceImpl) Update(ctx context.Context, userID, taskID int64, req domain.UpdateTaskRequest) (*domain.Task, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}

	member, err := s.teamRepo.GetMember(ctx, task.TeamID, userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, apperror.ErrNotTeamMember
	}

	err = s.txManager.WithTransaction(ctx, func(ctx context.Context) error {
		if req.Title != nil && *req.Title != task.Title {
			s.recordHistory(ctx, taskID, userID, "title", task.Title, *req.Title)
			task.Title = *req.Title
		}
		if req.Description != nil && *req.Description != task.Description {
			s.recordHistory(ctx, taskID, userID, "description", task.Description, *req.Description)
			task.Description = *req.Description
		}
		if req.Status != nil && *req.Status != string(task.Status) {
			s.recordHistory(ctx, taskID, userID, "status", string(task.Status), *req.Status)
			task.Status = domain.TaskStatus(*req.Status)
		}
		if req.Priority != nil && domain.TaskPriority(*req.Priority) != task.Priority {
			s.recordHistory(ctx, taskID, userID, "priority", fmt.Sprintf("%d", task.Priority), fmt.Sprintf("%d", *req.Priority))
			task.Priority = domain.TaskPriority(*req.Priority)
		}
		if req.AssigneeID != nil {
			oldVal := "unassigned"
			if task.AssigneeID.Valid {
				oldVal = fmt.Sprintf("%d", task.AssigneeID.Int64)
			}
			newVal := fmt.Sprintf("%d", *req.AssigneeID)
			s.recordHistory(ctx, taskID, userID, "assignee_id", oldVal, newVal)
			task.AssigneeID = sql.NullInt64{Int64: *req.AssigneeID, Valid: true}
		}
		if req.DueDate != nil {
			oldVal := "none"
			if task.DueDate.Valid {
				oldVal = task.DueDate.Time.Format("2006-01-02")
			}
			t, err := time.Parse("2006-01-02", *req.DueDate)
			if err != nil {
				return apperror.BadRequest("invalid due_date format, use YYYY-MM-DD")
			}
			s.recordHistory(ctx, taskID, userID, "due_date", oldVal, *req.DueDate)
			task.DueDate = sql.NullTime{Time: t, Valid: true}
		}

		return s.taskRepo.Update(ctx, task)
	})
	if err != nil {
		return nil, err
	}

	_ = s.taskCache.InvalidateTeam(ctx, task.TeamID)

	return s.taskRepo.GetByID(ctx, taskID)
}

func (s *TaskServiceImpl) recordHistory(ctx context.Context, taskID, userID int64, field, oldVal, newVal string) {
	_ = s.historyRepo.Create(ctx, &domain.TaskHistory{
		TaskID:   taskID,
		UserID:   userID,
		Field:    field,
		OldValue: oldVal,
		NewValue: newVal,
	})
}

func (s *TaskServiceImpl) List(ctx context.Context, userID int64, filter domain.TaskFilter) (*domain.TaskListResponse, error) {
	if filter.TeamID > 0 {
		member, err := s.teamRepo.GetMember(ctx, filter.TeamID, userID)
		if err != nil {
			return nil, err
		}
		if member == nil {
			return nil, apperror.ErrNotTeamMember
		}
	}

	cached, err := s.taskCache.GetTaskList(ctx, filter)
	if err == nil && cached != nil {
		return cached, nil
	}

	tasks, total, err := s.taskRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if tasks == nil {
		tasks = []domain.Task{}
	}

	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	totalPages := (total + pageSize - 1) / pageSize

	response := &domain.TaskListResponse{
		Tasks:      tasks,
		Total:      total,
		Page:       filter.Page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}

	_ = s.taskCache.SetTaskList(ctx, filter, response)

	return response, nil
}

func (s *TaskServiceImpl) GetHistory(ctx context.Context, userID, taskID int64) ([]domain.TaskHistory, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}

	member, err := s.teamRepo.GetMember(ctx, task.TeamID, userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, apperror.ErrNotTeamMember
	}

	return s.historyRepo.ListByTaskID(ctx, taskID)
}

func (s *TaskServiceImpl) GetOrphanedAssignees(ctx context.Context) ([]domain.OrphanedAssignee, error) {
	return s.taskRepo.GetOrphanedAssignees(ctx)
}

package service

import (
	"context"

	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
	"github.com/shalfey088/team-task-nexus/internal/port"
)

type CommentServiceImpl struct {
	commentRepo port.CommentRepository
	taskRepo    port.TaskRepository
	teamRepo    port.TeamRepository
	notifSvc    port.NotificationService
}

func NewCommentService(
	commentRepo port.CommentRepository,
	taskRepo port.TaskRepository,
	teamRepo port.TeamRepository,
	notifSvc port.NotificationService,
) *CommentServiceImpl {
	return &CommentServiceImpl{
		commentRepo: commentRepo,
		taskRepo:    taskRepo,
		teamRepo:    teamRepo,
		notifSvc:    notifSvc,
	}
}

func (s *CommentServiceImpl) Create(ctx context.Context, userID, taskID int64, req domain.CreateCommentRequest) (*domain.TaskComment, error) {
	if req.Content == "" {
		return nil, apperror.BadRequest("comment content is required")
	}

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

	comment := &domain.TaskComment{
		TaskID:  taskID,
		UserID:  userID,
		Content: req.Content,
	}

	id, err := s.commentRepo.Create(ctx, comment)
	if err != nil {
		return nil, err
	}
	comment.ID = id

	_ = s.notifSvc.NotifyCommentAdded(ctx, comment, task)

	return comment, nil
}

func (s *CommentServiceImpl) ListByTaskID(ctx context.Context, userID, taskID int64) ([]domain.TaskComment, error) {
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

	return s.commentRepo.ListByTaskID(ctx, taskID)
}

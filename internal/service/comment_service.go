package service

import (
	"context"
	"errors"
	"strings"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// CommentDeps lists the collaborators of CommentService.
type CommentDeps struct {
	Comments port.CommentRepository
	Tasks    port.TaskRepository
	Teams    port.TeamRepository
	Notifier port.NotificationService
}

// CommentService implements port.CommentService.
type CommentService struct {
	comments port.CommentRepository
	tasks    port.TaskRepository
	teams    port.TeamRepository
	notifier port.NotificationService
}

// NewCommentService validates deps and returns a CommentService.
func NewCommentService(deps CommentDeps) (*CommentService, error) {
	var missing string
	switch {
	case deps.Comments == nil:
		missing = "comment repository"
	case deps.Tasks == nil:
		missing = "task repository"
	case deps.Teams == nil:
		missing = "team repository"
	case deps.Notifier == nil:
		missing = "notification service"
	}
	if missing != "" {
		return nil, errors.New("comment service: " + missing + " is required")
	}
	return &CommentService{comments: deps.Comments, tasks: deps.Tasks, teams: deps.Teams, notifier: deps.Notifier}, nil
}

// Create adds a comment by userID to a task of one of the user's teams.
func (s *CommentService) Create(ctx context.Context, userID, taskID int64, in port.CreateCommentInput) (*domain.Comment, error) {
	content := strings.TrimSpace(in.Content)
	errs := fieldErrors{}
	errs.checkRequiredText("content", content, maxCommentChars)
	if err := errs.err(); err != nil {
		return nil, err
	}

	task, err := loadTask(ctx, s.tasks, s.teams, taskID, userID)
	if err != nil {
		return nil, err
	}
	id, err := s.comments.Create(ctx, &domain.Comment{TaskID: taskID, UserID: userID, Content: content})
	if err != nil {
		return nil, err
	}
	// The row is read back for the timestamps the database assigned.
	comment, err := s.comments.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.notifier.NotifyCommentAdded(ctx, comment, task); err != nil {
		logger.FromContext(ctx).Warn("comment notification failed", "task_id", taskID, "comment_id", id, "error", err)
	}
	return comment, nil
}

// List returns the comments of a task to a member of its team.
func (s *CommentService) List(ctx context.Context, userID, taskID int64) ([]domain.Comment, error) {
	if _, err := loadTask(ctx, s.tasks, s.teams, taskID, userID); err != nil {
		return nil, err
	}
	return s.comments.ListByTask(ctx, taskID)
}

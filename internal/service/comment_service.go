package service

import (
	"context"
	"errors"
	"strings"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// CommentDeps lists the collaborators of CommentService.
type CommentDeps struct {
	Comments port.CommentRepository
	Tasks    port.TaskRepository
	Teams    port.TeamRepository
	Outbox   port.OutboxRepository
	Tx       port.TxManager
}

// CommentService implements port.CommentService. Every new comment writes a task.commented
// event to the outbox in the transaction that inserts it.
type CommentService struct {
	comments port.CommentRepository
	tasks    port.TaskRepository
	teams    port.TeamRepository
	outbox   port.OutboxRepository
	tx       port.TxManager
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
	case deps.Outbox == nil:
		missing = "outbox repository"
	case deps.Tx == nil:
		missing = "transaction manager"
	}
	if missing != "" {
		return nil, errors.New("comment service: " + missing + " is required")
	}
	return &CommentService{
		comments: deps.Comments,
		tasks:    deps.Tasks,
		teams:    deps.Teams,
		outbox:   deps.Outbox,
		tx:       deps.Tx,
	}, nil
}

// Create adds a comment by userID to a task of one of the user's teams.
func (s *CommentService) Create(ctx context.Context, userID, taskID int64, in port.CreateCommentInput) (*domain.Comment, error) {
	content := strings.TrimSpace(in.Content)
	errs := fieldErrors{}
	errs.checkRequiredText("content", content, maxCommentChars)
	if err := errs.err(); err != nil {
		return nil, err
	}

	var comment *domain.Comment
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		comment, err = s.create(ctx, userID, taskID, content)
		return err
	})
	if err != nil {
		return nil, err
	}
	return comment, nil
}

// create is the transactional part of Create.
func (s *CommentService) create(ctx context.Context, userID, taskID int64, content string) (*domain.Comment, error) {
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
	event, err := taskCommentedEvent(domain.TaskCommented{
		TaskID:         task.ID,
		TeamID:         task.TeamID,
		CommentID:      comment.ID,
		AuthorID:       userID,
		TaskCreatorID:  task.CreatorID,
		TaskAssigneeID: task.AssigneeID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.outbox.Add(ctx, event); err != nil {
		return nil, err
	}
	return comment, nil
}

// List returns a page of the comments of a task to a member of its team.
func (s *CommentService) List(ctx context.Context, userID, taskID int64, p domain.PageRequest) (*domain.CommentPage, error) {
	if err := normalizePageRequest(&p); err != nil {
		return nil, err
	}
	if _, err := loadTask(ctx, s.tasks, s.teams, taskID, userID); err != nil {
		return nil, err
	}
	comments, total, err := s.comments.ListByTask(ctx, taskID, p)
	if err != nil {
		return nil, err
	}
	return &domain.CommentPage{
		Comments:   comments,
		Total:      total,
		Page:       p.Page,
		PageSize:   p.PageSize,
		TotalPages: totalPages(total, p.PageSize),
	}, nil
}

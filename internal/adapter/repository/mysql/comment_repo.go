package mysql

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
)

type CommentRepo struct {
	db *sqlx.DB
}

func NewCommentRepo(db *sqlx.DB) *CommentRepo {
	return &CommentRepo{db: db}
}

func (r *CommentRepo) Create(ctx context.Context, comment *domain.TaskComment) (int64, error) {
	q := getQuerier(ctx, r.db)
	result, err := q.ExecContext(ctx,
		"INSERT INTO task_comments (task_id, user_id, content) VALUES (?, ?, ?)",
		comment.TaskID, comment.UserID, comment.Content,
	)
	if err != nil {
		return 0, apperror.Internal("create comment", err)
	}
	return result.LastInsertId()
}

func (r *CommentRepo) ListByTaskID(ctx context.Context, taskID int64) ([]domain.TaskComment, error) {
	q := getQuerier(ctx, r.db)
	var comments []domain.TaskComment
	err := q.SelectContext(ctx, &comments,
		"SELECT * FROM task_comments WHERE task_id = ? ORDER BY created_at ASC",
		taskID,
	)
	if err != nil {
		return nil, apperror.Internal("list comments", err)
	}
	return comments, nil
}

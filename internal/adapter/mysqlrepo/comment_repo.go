package mysqlrepo

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const commentColumns = "id, task_id, user_id, content, created_at, updated_at"

type commentRow struct {
	ID        int64     `db:"id"`
	TaskID    int64     `db:"task_id"`
	UserID    int64     `db:"user_id"`
	Content   string    `db:"content"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// CommentRepo stores task comments.
type CommentRepo struct {
	db *sqlx.DB
}

// NewCommentRepo returns a CommentRepo on db.
func NewCommentRepo(db *sqlx.DB) *CommentRepo {
	return &CommentRepo{db: db}
}

// Create inserts comment and returns its id.
func (r *CommentRepo) Create(ctx context.Context, comment *domain.Comment) (int64, error) {
	result, err := getQuerier(ctx, r.db).ExecContext(ctx,
		"INSERT INTO task_comments (task_id, user_id, content) VALUES (?, ?, ?)",
		comment.TaskID, comment.UserID, comment.Content,
	)
	if err != nil {
		return 0, apperror.Internal("create comment", err)
	}
	return insertedID(result, "create comment")
}

// GetByID returns the comment with the given id.
func (r *CommentRepo) GetByID(ctx context.Context, id int64) (*domain.Comment, error) {
	var row commentRow
	err := getQuerier(ctx, r.db).GetContext(ctx, &row, "SELECT "+commentColumns+" FROM task_comments WHERE id = ?", id)
	if err != nil {
		return nil, lookupError(err, "get comment", "comment", id)
	}
	comment := domain.Comment(row)
	return &comment, nil
}

// ListByTask returns page p of the comments of the task, oldest first, and the number of all
// its comments. Comments posted within the same second share their timestamp, so the id breaks
// the tie and keeps the pages from overlapping.
func (r *CommentRepo) ListByTask(ctx context.Context, taskID int64, p domain.PageRequest) ([]domain.Comment, int, error) {
	limit, offset, err := pageWindow(p, "list comments")
	if err != nil {
		return nil, 0, err
	}
	conn := getQuerier(ctx, r.db)

	var total int
	if err = conn.GetContext(ctx, &total, "SELECT COUNT(*) FROM task_comments WHERE task_id = ?", taskID); err != nil {
		return nil, 0, apperror.Internal("count comments", err)
	}

	var rows []commentRow
	err = conn.SelectContext(ctx, &rows,
		"SELECT "+commentColumns+" FROM task_comments WHERE task_id = ? ORDER BY created_at ASC, id ASC LIMIT ? OFFSET ?",
		taskID, limit, offset,
	)
	if err != nil {
		return nil, 0, apperror.Internal("list comments", err)
	}
	comments := make([]domain.Comment, 0, len(rows))
	for _, row := range rows {
		comments = append(comments, domain.Comment(row))
	}
	return comments, total, nil
}

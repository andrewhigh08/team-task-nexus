package mysqlrepo

import (
	"context"

	"github.com/jmoiron/sqlx"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// TaskHistoryRepo stores the change history of tasks.
type TaskHistoryRepo struct {
	db *sqlx.DB
}

// NewTaskHistoryRepo returns a TaskHistoryRepo on db.
func NewTaskHistoryRepo(db *sqlx.DB) *TaskHistoryRepo {
	return &TaskHistoryRepo{db: db}
}

// Create inserts one history entry.
func (r *TaskHistoryRepo) Create(ctx context.Context, history *domain.TaskHistory) error {
	q := getQuerier(ctx, r.db)
	_, err := q.ExecContext(ctx,
		`INSERT INTO task_history (task_id, user_id, field, old_value, new_value)
		 VALUES (?, ?, ?, ?, ?)`,
		history.TaskID, history.UserID, history.Field, history.OldValue, history.NewValue,
	)
	if err != nil {
		return apperror.Internal("create task history", err)
	}
	return nil
}

// ListByTaskID returns the history of a task, newest first.
func (r *TaskHistoryRepo) ListByTaskID(ctx context.Context, taskID int64) ([]domain.TaskHistory, error) {
	q := getQuerier(ctx, r.db)
	var history []domain.TaskHistory
	err := q.SelectContext(ctx, &history,
		"SELECT * FROM task_history WHERE task_id = ? ORDER BY changed_at DESC",
		taskID,
	)
	if err != nil {
		return nil, apperror.Internal("list task history", err)
	}
	return history, nil
}

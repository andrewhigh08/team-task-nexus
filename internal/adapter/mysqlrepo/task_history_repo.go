package mysqlrepo

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

type taskChangeRow struct {
	ID        int64     `db:"id"`
	TaskID    int64     `db:"task_id"`
	UserID    int64     `db:"user_id"`
	Field     string    `db:"field"`
	OldValue  string    `db:"old_value"`
	NewValue  string    `db:"new_value"`
	ChangedAt time.Time `db:"changed_at"`
}

// TaskHistoryRepo stores the change history of tasks.
type TaskHistoryRepo struct {
	db *sqlx.DB
}

// NewTaskHistoryRepo returns a TaskHistoryRepo on db.
func NewTaskHistoryRepo(db *sqlx.DB) *TaskHistoryRepo {
	return &TaskHistoryRepo{db: db}
}

// Add inserts changes with a single multi-row INSERT, so they share one timestamp.
func (r *TaskHistoryRepo) Add(ctx context.Context, changes []domain.TaskChange) error {
	if len(changes) == 0 {
		return nil
	}
	rows := make([]taskChangeRow, 0, len(changes))
	for _, c := range changes {
		rows = append(rows, taskChangeRow{TaskID: c.TaskID, UserID: c.UserID, Field: c.Field, OldValue: c.OldValue, NewValue: c.NewValue})
	}
	// sqlx expands the VALUES clause to one tuple per row.
	_, err := getQuerier(ctx, r.db).NamedExecContext(ctx, `
		INSERT INTO task_history (task_id, user_id, field, old_value, new_value)
		VALUES (:task_id, :user_id, :field, :old_value, :new_value)`, rows,
	)
	if err != nil {
		return apperror.Internal("add task history", err)
	}
	return nil
}

// ListByTask returns the history of the task, newest first.
func (r *TaskHistoryRepo) ListByTask(ctx context.Context, taskID int64) ([]domain.TaskChange, error) {
	var rows []taskChangeRow
	err := getQuerier(ctx, r.db).SelectContext(ctx, &rows, `
		SELECT id, task_id, user_id, field, old_value, new_value, changed_at
		FROM task_history
		WHERE task_id = ?
		ORDER BY changed_at DESC, id DESC`, taskID,
	)
	if err != nil {
		return nil, apperror.Internal("list task history", err)
	}
	changes := make([]domain.TaskChange, 0, len(rows))
	for _, row := range rows {
		changes = append(changes, domain.TaskChange(row))
	}
	return changes, nil
}

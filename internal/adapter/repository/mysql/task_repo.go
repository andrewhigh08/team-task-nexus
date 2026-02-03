package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
)

type TaskRepo struct {
	db *sqlx.DB
}

func NewTaskRepo(db *sqlx.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

func (r *TaskRepo) Create(ctx context.Context, task *domain.Task) (int64, error) {
	q := getQuerier(ctx, r.db)
	result, err := q.ExecContext(ctx,
		`INSERT INTO tasks (title, description, status, priority, team_id, creator_id, assignee_id, due_date)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		task.Title, task.Description, task.Status, task.Priority,
		task.TeamID, task.CreatorID, task.AssigneeID, task.DueDate,
	)
	if err != nil {
		return 0, apperror.Internal("create task", err)
	}
	return result.LastInsertId()
}

func (r *TaskRepo) GetByID(ctx context.Context, id int64) (*domain.Task, error) {
	q := getQuerier(ctx, r.db)
	var task domain.Task
	err := q.GetContext(ctx, &task, "SELECT * FROM tasks WHERE id = ?", id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NotFound("task not found")
		}
		return nil, apperror.Internal("get task", err)
	}
	return &task, nil
}

func (r *TaskRepo) Update(ctx context.Context, task *domain.Task) error {
	q := getQuerier(ctx, r.db)
	_, err := q.ExecContext(ctx,
		`UPDATE tasks SET title = ?, description = ?, status = ?, priority = ?,
		 assignee_id = ?, due_date = ?, updated_at = NOW()
		 WHERE id = ?`,
		task.Title, task.Description, task.Status, task.Priority,
		task.AssigneeID, task.DueDate, task.ID,
	)
	if err != nil {
		return apperror.Internal("update task", err)
	}
	return nil
}

func (r *TaskRepo) List(ctx context.Context, filter domain.TaskFilter) ([]domain.Task, int, error) {
	q := getQuerier(ctx, r.db)

	var conditions []string
	var args []interface{}

	if filter.TeamID > 0 {
		conditions = append(conditions, "team_id = ?")
		args = append(args, filter.TeamID)
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.AssigneeID > 0 {
		conditions = append(conditions, "assignee_id = ?")
		args = append(args, filter.AssigneeID)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM tasks %s", where)
	err := q.GetContext(ctx, &total, countQuery, args...)
	if err != nil {
		return nil, 0, apperror.Internal("count tasks", err)
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	offset := (filter.Page - 1) * filter.PageSize
	listQuery := fmt.Sprintf("SELECT * FROM tasks %s ORDER BY created_at DESC LIMIT ? OFFSET ?", where)
	args = append(args, filter.PageSize, offset)

	var tasks []domain.Task
	err = q.SelectContext(ctx, &tasks, listQuery, args...)
	if err != nil {
		return nil, 0, apperror.Internal("list tasks", err)
	}

	return tasks, total, nil
}

func (r *TaskRepo) GetOrphanedAssignees(ctx context.Context) ([]domain.OrphanedAssignee, error) {
	q := getQuerier(ctx, r.db)
	var result []domain.OrphanedAssignee
	err := q.SelectContext(ctx, &result, `
		SELECT tk.id, tk.title, tk.assignee_id, u.full_name
		FROM tasks tk
		JOIN users u ON u.id = tk.assignee_id
		WHERE tk.assignee_id IS NOT NULL
			AND NOT EXISTS (
				SELECT 1 FROM team_members tm
				WHERE tm.team_id = tk.team_id AND tm.user_id = tk.assignee_id
			)`,
	)
	if err != nil {
		return nil, apperror.Internal("get orphaned assignees", err)
	}
	return result, nil
}

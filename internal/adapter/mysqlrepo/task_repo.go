package mysqlrepo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

const taskColumns = "id, team_id, title, description, status, priority, creator_id, assignee_id, " +
	"due_date, completed_at, created_at, updated_at"

// viewerTeamsCondition limits a task query to the teams of the viewer. Every listing starts
// with it, so no filter combination can reach another team's tasks.
const viewerTeamsCondition = "team_id IN (SELECT team_id FROM team_members WHERE user_id = ?)"

type taskRow struct {
	ID          int64         `db:"id"`
	TeamID      int64         `db:"team_id"`
	Title       string        `db:"title"`
	Description string        `db:"description"`
	Status      string        `db:"status"`
	Priority    int           `db:"priority"`
	CreatorID   int64         `db:"creator_id"`
	AssigneeID  sql.NullInt64 `db:"assignee_id"`
	DueDate     sql.NullTime  `db:"due_date"`
	CompletedAt sql.NullTime  `db:"completed_at"`
	CreatedAt   time.Time     `db:"created_at"`
	UpdatedAt   time.Time     `db:"updated_at"`
}

func (r taskRow) toDomain() domain.Task {
	return domain.Task{
		ID:          r.ID,
		TeamID:      r.TeamID,
		Title:       r.Title,
		Description: r.Description,
		Status:      domain.TaskStatus(r.Status),
		Priority:    domain.TaskPriority(r.Priority),
		CreatorID:   r.CreatorID,
		AssigneeID:  nullInt64Ptr(r.AssigneeID),
		DueDate:     nullTimePtr(r.DueDate),
		CompletedAt: nullTimePtr(r.CompletedAt),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

type orphanedRow struct {
	TaskID       int64  `db:"task_id"`
	TaskTitle    string `db:"task_title"`
	AssigneeID   int64  `db:"assignee_id"`
	AssigneeName string `db:"assignee_name"`
}

// TaskRepo stores tasks.
type TaskRepo struct {
	db *sqlx.DB
}

// NewTaskRepo returns a TaskRepo on db.
func NewTaskRepo(db *sqlx.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

// Create inserts task and returns its id.
func (r *TaskRepo) Create(ctx context.Context, task *domain.Task) (int64, error) {
	result, err := getQuerier(ctx, r.db).ExecContext(ctx, `
		INSERT INTO tasks (team_id, title, description, status, priority, creator_id, assignee_id, due_date, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.TeamID, task.Title, task.Description, string(task.Status), int(task.Priority),
		task.CreatorID, task.AssigneeID, dateArg(task.DueDate), task.CompletedAt,
	)
	if err != nil {
		return 0, apperror.Internal("create task", err)
	}
	return insertedID(result, "create task")
}

// GetByID returns the task with the given id.
func (r *TaskRepo) GetByID(ctx context.Context, id int64) (*domain.Task, error) {
	return r.get(ctx, getQuerier(ctx, r.db), "SELECT "+taskColumns+" FROM tasks WHERE id = ?", id)
}

// GetByIDForUpdate returns the task with the given id and locks its row until the
// transaction in ctx ends, so concurrent updates of the task apply one after the other.
func (r *TaskRepo) GetByIDForUpdate(ctx context.Context, id int64) (*domain.Task, error) {
	tx, ok := txFrom(ctx)
	if !ok {
		// Outside a transaction the lock would be released as soon as the SELECT returns.
		return nil, apperror.Internal("lock task", fmt.Errorf("task %d locked outside a transaction", id))
	}
	return r.get(ctx, tx, "SELECT "+taskColumns+" FROM tasks WHERE id = ? FOR UPDATE", id)
}

func (r *TaskRepo) get(ctx context.Context, q querier, query string, id int64) (*domain.Task, error) {
	var row taskRow
	if err := q.GetContext(ctx, &row, query, id); err != nil {
		return nil, lookupError(err, "get task", "task", id)
	}
	task := row.toDomain()
	return &task, nil
}

// Update writes every mutable field of task.
func (r *TaskRepo) Update(ctx context.Context, task *domain.Task) error {
	result, err := getQuerier(ctx, r.db).ExecContext(ctx, `
		UPDATE tasks
		SET title = ?, description = ?, status = ?, priority = ?, assignee_id = ?, due_date = ?,
			completed_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		task.Title, task.Description, string(task.Status), int(task.Priority), task.AssigneeID,
		dateArg(task.DueDate), task.CompletedAt, task.ID,
	)
	if err != nil {
		return apperror.Internal("update task", err)
	}
	updated, err := affectedOne(result, "update task")
	if err != nil {
		return err
	}
	if !updated {
		return apperror.NotFound("task", task.ID)
	}
	return nil
}

// List returns the page of tasks q selects, newest first, and the number of all matching
// tasks. Only tasks of the teams q.ViewerID is a member of are selected.
func (r *TaskRepo) List(ctx context.Context, q domain.TaskQuery) ([]domain.Task, int, error) {
	if q.Page < 1 || q.PageSize < 1 {
		return nil, 0, apperror.Internal("list tasks", fmt.Errorf("invalid page %d of size %d", q.Page, q.PageSize))
	}
	where, args := taskListFilter(q)
	conn := getQuerier(ctx, r.db)

	var total int
	if err := conn.GetContext(ctx, &total, "SELECT COUNT(*) FROM tasks WHERE "+where, args...); err != nil {
		return nil, 0, apperror.Internal("count tasks", err)
	}

	var rows []taskRow
	query := "SELECT " + taskColumns + " FROM tasks WHERE " + where +
		" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	if err := conn.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, 0, apperror.Internal("list tasks", err)
	}

	tasks := make([]domain.Task, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, row.toDomain())
	}
	return tasks, total, nil
}

// taskListFilter returns the WHERE conditions of q and their arguments.
func taskListFilter(q domain.TaskQuery) (string, []any) {
	conditions := []string{viewerTeamsCondition}
	args := []any{q.ViewerID}
	if q.TeamID > 0 {
		conditions = append(conditions, "team_id = ?")
		args = append(args, q.TeamID)
	}
	if q.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, string(q.Status))
	}
	if q.AssigneeID > 0 {
		conditions = append(conditions, "assignee_id = ?")
		args = append(args, q.AssigneeID)
	}
	return strings.Join(conditions, " AND "), args
}

// ListOrphaned returns the tasks of the team assigned to users who are no longer members.
func (r *TaskRepo) ListOrphaned(ctx context.Context, teamID int64) ([]domain.OrphanedAssignment, error) {
	var rows []orphanedRow
	err := getQuerier(ctx, r.db).SelectContext(ctx, &rows, `
		SELECT k.id AS task_id, k.title AS task_title, k.assignee_id, u.full_name AS assignee_name
		FROM tasks k
		JOIN users u ON u.id = k.assignee_id
		WHERE k.team_id = ?
			AND NOT EXISTS (
				SELECT 1 FROM team_members m
				WHERE m.team_id = k.team_id AND m.user_id = k.assignee_id
			)
		ORDER BY k.id`, teamID,
	)
	if err != nil {
		return nil, apperror.Internal("list orphaned assignments", err)
	}
	orphaned := make([]domain.OrphanedAssignment, 0, len(rows))
	for _, row := range rows {
		orphaned = append(orphaned, domain.OrphanedAssignment(row))
	}
	return orphaned, nil
}

func nullInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

func nullTimePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	return &n.Time
}

// dateArg formats a due date for the DATE column; a time of day would be truncated with a
// warning.
func dateArg(date *time.Time) any {
	if date == nil {
		return nil
	}
	return date.Format(time.DateOnly)
}

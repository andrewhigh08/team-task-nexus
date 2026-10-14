package handler

import (
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/opt"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

const fieldDueDate = "due_date"

type createTaskRequest struct {
	TeamID      int64                `json:"team_id"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Priority    *domain.TaskPriority `json:"priority"`
	AssigneeID  *int64               `json:"assignee_id"`
	DueDate     *string              `json:"due_date"`
}

func (req createTaskRequest) toInput() (port.CreateTaskInput, error) {
	in := port.CreateTaskInput{
		TeamID:      req.TeamID,
		Title:       req.Title,
		Description: req.Description,
		Priority:    req.Priority,
		AssigneeID:  req.AssigneeID,
	}
	if req.DueDate != nil {
		date, err := parseDate(fieldDueDate, *req.DueDate)
		if err != nil {
			return port.CreateTaskInput{}, err
		}
		in.DueDate = &date
	}
	return in, nil
}

// updateTaskRequest is the body of PATCH /tasks/{id}: absent fields are kept, and null clears
// assignee_id and due_date.
type updateTaskRequest struct {
	Title       opt.Field[string]              `json:"title"`
	Description opt.Field[string]              `json:"description"`
	Status      opt.Field[domain.TaskStatus]   `json:"status"`
	Priority    opt.Field[domain.TaskPriority] `json:"priority"`
	AssigneeID  opt.Field[int64]               `json:"assignee_id"`
	DueDate     opt.Field[string]              `json:"due_date"`
}

func (req updateTaskRequest) toInput() (port.UpdateTaskInput, error) {
	dueDate, err := parseDueDate(req.DueDate)
	if err != nil {
		return port.UpdateTaskInput{}, err
	}
	return port.UpdateTaskInput{
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		Priority:    req.Priority,
		AssigneeID:  req.AssigneeID,
		DueDate:     dueDate,
	}, nil
}

func parseDueDate(f opt.Field[string]) (opt.Field[time.Time], error) {
	if !f.Set || f.Null {
		return opt.Field[time.Time]{Set: f.Set, Null: f.Null}, nil
	}
	date, err := parseDate(fieldDueDate, f.Value)
	if err != nil {
		return opt.Field[time.Time]{}, err
	}
	return opt.Of(date), nil
}

type taskResponse struct {
	ID          int64      `json:"id"`
	TeamID      int64      `json:"team_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Priority    int        `json:"priority"`
	CreatorID   int64      `json:"creator_id"`
	AssigneeID  *int64     `json:"assignee_id"`
	DueDate     *string    `json:"due_date"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func newTaskResponse(t domain.Task) taskResponse {
	resp := taskResponse{
		ID:          t.ID,
		TeamID:      t.TeamID,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		Priority:    int(t.Priority),
		CreatorID:   t.CreatorID,
		AssigneeID:  t.AssigneeID,
		CreatedAt:   t.CreatedAt.UTC(),
		UpdatedAt:   t.UpdatedAt.UTC(),
	}
	if t.DueDate != nil {
		date := t.DueDate.Format(time.DateOnly)
		resp.DueDate = &date
	}
	if t.CompletedAt != nil {
		completedAt := t.CompletedAt.UTC()
		resp.CompletedAt = &completedAt
	}
	return resp
}

type taskChangeResponse struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	UserID    int64     `json:"user_id"`
	Field     string    `json:"field"`
	OldValue  string    `json:"old_value"`
	NewValue  string    `json:"new_value"`
	ChangedAt time.Time `json:"changed_at"`
}

func newTaskChangeResponse(c domain.TaskChange) taskChangeResponse {
	return taskChangeResponse{
		ID:        c.ID,
		TaskID:    c.TaskID,
		UserID:    c.UserID,
		Field:     c.Field,
		OldValue:  c.OldValue,
		NewValue:  c.NewValue,
		ChangedAt: c.ChangedAt.UTC(),
	}
}

type orphanedAssignmentResponse struct {
	TaskID       int64  `json:"task_id"`
	TaskTitle    string `json:"task_title"`
	AssigneeID   int64  `json:"assignee_id"`
	AssigneeName string `json:"assignee_name"`
}

func newOrphanedAssignmentResponse(o domain.OrphanedAssignment) orphanedAssignmentResponse {
	return orphanedAssignmentResponse(o)
}

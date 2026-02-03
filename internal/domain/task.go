package domain

import (
	"database/sql"
	"time"
)

type TaskStatus string

const (
	TaskStatusTodo       TaskStatus = "todo"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusReview     TaskStatus = "review"
	TaskStatusDone       TaskStatus = "done"
)

type TaskPriority int

const (
	TaskPriorityLow    TaskPriority = 1
	TaskPriorityMedium TaskPriority = 2
	TaskPriorityHigh   TaskPriority = 3
)

type Task struct {
	ID          int64          `json:"id" db:"id"`
	Title       string         `json:"title" db:"title"`
	Description string         `json:"description" db:"description"`
	Status      TaskStatus     `json:"status" db:"status"`
	Priority    TaskPriority   `json:"priority" db:"priority"`
	TeamID      int64          `json:"team_id" db:"team_id"`
	CreatorID   int64          `json:"creator_id" db:"creator_id"`
	AssigneeID  sql.NullInt64  `json:"assignee_id" db:"assignee_id"`
	DueDate     sql.NullTime   `json:"due_date" db:"due_date"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}

type CreateTaskRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	TeamID      int64  `json:"team_id"`
	AssigneeID  *int64 `json:"assignee_id,omitempty"`
	DueDate     string `json:"due_date,omitempty"`
}

type UpdateTaskRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	Priority    *int    `json:"priority,omitempty"`
	AssigneeID  *int64  `json:"assignee_id,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
}

type TaskFilter struct {
	TeamID     int64  `json:"team_id"`
	Status     string `json:"status"`
	AssigneeID int64  `json:"assignee_id"`
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
}

type TaskListResponse struct {
	Tasks      []Task `json:"tasks"`
	Total      int    `json:"total"`
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
	TotalPages int    `json:"total_pages"`
}

type OrphanedAssignee struct {
	TaskID       int64  `json:"task_id" db:"id"`
	TaskTitle    string `json:"task_title" db:"title"`
	AssigneeID   int64  `json:"assignee_id" db:"assignee_id"`
	AssigneeName string `json:"assignee_name" db:"full_name"`
}

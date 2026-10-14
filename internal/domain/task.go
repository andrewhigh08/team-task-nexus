package domain

import "time"

// TaskStatus is the column of the team board a task is in.
type TaskStatus string

// Task statuses. A task may move from any status to any other.
const (
	TaskStatusTodo       TaskStatus = "todo"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusReview     TaskStatus = "review"
	TaskStatusDone       TaskStatus = "done"
)

// Valid reports whether s is one of the defined statuses.
func (s TaskStatus) Valid() bool {
	switch s {
	case TaskStatusTodo, TaskStatusInProgress, TaskStatusReview, TaskStatusDone:
		return true
	}
	return false
}

// TaskPriority orders tasks by urgency; a higher value is more urgent.
type TaskPriority int

// Task priorities.
const (
	TaskPriorityLow    TaskPriority = 1
	TaskPriorityMedium TaskPriority = 2
	TaskPriorityHigh   TaskPriority = 3
)

// Valid reports whether p is one of the defined priorities.
func (p TaskPriority) Valid() bool {
	return p >= TaskPriorityLow && p <= TaskPriorityHigh
}

// Task is a unit of work of a team. AssigneeID and DueDate are nil when unset; DueDate is a
// calendar date, held as midnight UTC. CompletedAt is set while the task is done.
type Task struct {
	ID          int64
	TeamID      int64
	Title       string
	Description string
	Status      TaskStatus
	Priority    TaskPriority
	CreatorID   int64
	AssigneeID  *int64
	DueDate     *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SetStatus moves the task to status at the instant now. CompletedAt is set when the task
// becomes done and cleared when it leaves done, so it always tells when the current
// completion happened.
func (t *Task) SetStatus(status TaskStatus, now time.Time) {
	switch {
	case status != TaskStatusDone:
		t.CompletedAt = nil
	case t.Status != TaskStatusDone:
		completedAt := now.UTC()
		t.CompletedAt = &completedAt
	}
	t.Status = status
}

// TaskQuery selects a page of the tasks visible to ViewerID: those of the teams the viewer is
// a member of. TeamID, Status and AssigneeID narrow the selection when they are not zero.
type TaskQuery struct {
	ViewerID   int64
	TeamID     int64
	Status     TaskStatus
	AssigneeID int64
	Page       int
	PageSize   int
}

// TaskPage is one page of the tasks matching a TaskQuery, newest first. Total counts all
// matching tasks.
type TaskPage struct {
	Tasks      []Task
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

package domain

import "time"

// TaskChange is one entry of a task's history: UserID changed Field from OldValue to NewValue.
// An empty value means the field had no value, such as a task without an assignee.
type TaskChange struct {
	ID        int64
	TaskID    int64
	UserID    int64
	Field     string
	OldValue  string
	NewValue  string
	ChangedAt time.Time
}

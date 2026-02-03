package domain

import "time"

type TaskHistory struct {
	ID        int64     `json:"id" db:"id"`
	TaskID    int64     `json:"task_id" db:"task_id"`
	UserID    int64     `json:"user_id" db:"user_id"`
	Field     string    `json:"field" db:"field"`
	OldValue  string    `json:"old_value" db:"old_value"`
	NewValue  string    `json:"new_value" db:"new_value"`
	ChangedAt time.Time `json:"changed_at" db:"changed_at"`
}

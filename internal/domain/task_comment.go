package domain

import "time"

type TaskComment struct {
	ID        int64     `json:"id" db:"id"`
	TaskID    int64     `json:"task_id" db:"task_id"`
	UserID    int64     `json:"user_id" db:"user_id"`
	Content   string    `json:"content" db:"content"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type CreateCommentRequest struct {
	Content string `json:"content"`
}

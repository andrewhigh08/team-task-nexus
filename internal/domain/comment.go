package domain

import "time"

// Comment is a message a team member left on a task.
type Comment struct {
	ID        int64
	TaskID    int64
	UserID    int64
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CommentPage is one page of the comments of a task, oldest first. Total counts all comments
// of the task.
type CommentPage struct {
	Comments   []Comment
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

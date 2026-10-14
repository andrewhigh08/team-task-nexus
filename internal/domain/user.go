package domain

import "time"

// User is a registered account. Email is stored normalized (trimmed, lower case) and
// PasswordHash is a bcrypt hash; the password itself is never kept.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	FullName     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

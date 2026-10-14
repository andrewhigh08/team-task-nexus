package domain

import "time"

// User is a registered account. Email is stored normalized (trimmed, lower case) and
// PasswordHash is a bcrypt hash; the password itself is never kept.
type User struct {
	ID           int64     `db:"id"`
	Email        string    `db:"email"`
	PasswordHash string    `db:"password_hash"`
	FullName     string    `db:"full_name"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

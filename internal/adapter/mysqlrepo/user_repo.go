package mysqlrepo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// UserRepo stores users.
type UserRepo struct {
	db *sqlx.DB
}

// NewUserRepo returns a UserRepo on db.
func NewUserRepo(db *sqlx.DB) *UserRepo {
	return &UserRepo{db: db}
}

// Create inserts user and returns its id.
func (r *UserRepo) Create(ctx context.Context, user *domain.User) (int64, error) {
	q := getQuerier(ctx, r.db)
	result, err := q.ExecContext(ctx,
		"INSERT INTO users (email, password_hash, full_name) VALUES (?, ?, ?)",
		user.Email, user.PasswordHash, user.FullName,
	)
	if err != nil {
		if isDuplicateEntry(err) {
			return 0, apperror.Conflict("user", "email", user.Email)
		}
		return 0, apperror.Internal("create user", err)
	}
	return result.LastInsertId()
}

// GetByID returns the user with the given id.
func (r *UserRepo) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	q := getQuerier(ctx, r.db)
	var user domain.User
	err := q.GetContext(ctx, &user, "SELECT * FROM users WHERE id = ?", id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NotFound("user", id)
		}
		return nil, apperror.Internal("get user by id", err)
	}
	return &user, nil
}

// GetByEmail returns the user with the given email.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	q := getQuerier(ctx, r.db)
	var user domain.User
	err := q.GetContext(ctx, &user, "SELECT * FROM users WHERE email = ?", email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NotFound("user", email)
		}
		return nil, apperror.Internal("get user by email", err)
	}
	return &user, nil
}

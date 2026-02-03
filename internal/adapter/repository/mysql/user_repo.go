package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/shalfey088/team-task-nexus/internal/domain"
	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
)

type UserRepo struct {
	db *sqlx.DB
}

func NewUserRepo(db *sqlx.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Create(ctx context.Context, user *domain.User) (int64, error) {
	q := getQuerier(ctx, r.db)
	result, err := q.ExecContext(ctx,
		"INSERT INTO users (email, password_hash, full_name) VALUES (?, ?, ?)",
		user.Email, user.PasswordHash, user.FullName,
	)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return 0, apperror.ErrEmailTaken
		}
		return 0, apperror.Internal("create user", err)
	}
	return result.LastInsertId()
}

func (r *UserRepo) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	q := getQuerier(ctx, r.db)
	var user domain.User
	err := q.GetContext(ctx, &user, "SELECT * FROM users WHERE id = ?", id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NotFound("user not found")
		}
		return nil, apperror.Internal("get user by id", err)
	}
	return &user, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	q := getQuerier(ctx, r.db)
	var user domain.User
	err := q.GetContext(ctx, &user, "SELECT * FROM users WHERE email = ?", email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.NotFound("user not found")
		}
		return nil, apperror.Internal("get user by email", err)
	}
	return &user, nil
}

package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type ctxKey string

const txKey ctxKey = "tx"

type TransactionManager struct {
	db *sqlx.DB
}

func NewTransactionManager(db *sqlx.DB) *TransactionManager {
	return &TransactionManager{db: db}
}

func (m *TransactionManager) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := m.db.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	ctx = context.WithValue(ctx, txKey, tx)

	if err := fn(ctx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed: %v, original error: %w", rbErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

type querier interface {
	sqlx.QueryerContext
	sqlx.ExecerContext
	GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
}

func getQuerier(ctx context.Context, db *sqlx.DB) querier {
	if tx, ok := ctx.Value(txKey).(*sqlx.Tx); ok {
		return tx
	}
	return db
}

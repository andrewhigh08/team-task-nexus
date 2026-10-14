// Package mysqlrepo implements the repository ports, the transaction manager and the
// readiness check on MySQL with sqlx.
package mysqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// txKey is the context key of the transaction opened by TxManager.
type txKey struct{}

// TxManager implements port.TxManager with sqlx transactions carried in the context.
type TxManager struct {
	db *sqlx.DB
}

// NewTxManager returns a TxManager that opens transactions on db.
func NewTxManager(db *sqlx.DB) *TxManager {
	return &TxManager{db: db}
}

// WithinTx runs fn with a context that carries a transaction. When ctx already carries one,
// fn joins it and the outer call decides the outcome. Otherwise a new transaction commits
// when fn returns nil and rolls back when fn fails or panics; a panic keeps propagating
// after the rollback.
func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.withinTx(ctx, nil, fn)
}

// withinTx is WithinTx beginning a new transaction with opts. A transaction that fn joins
// keeps the options it was begun with.
func (m *TxManager) withinTx(ctx context.Context, opts *sql.TxOptions, fn func(ctx context.Context) error) error {
	if _, ok := txFrom(ctx); ok {
		return fn(ctx)
	}

	tx, err := m.db.BeginTxx(ctx, opts)
	if err != nil {
		return apperror.Internal("begin transaction", err)
	}
	// returned stays false only when fn panics; the rollback then runs while the panic
	// unwinds, without recovering it.
	returned := false
	defer func() {
		if !returned {
			_ = tx.Rollback()
		}
	}()

	err = fn(context.WithValue(ctx, txKey{}, tx))
	returned = true
	if err != nil {
		return rollback(tx, err)
	}
	if err := tx.Commit(); err != nil {
		return apperror.Internal("commit transaction", err)
	}
	return nil
}

// rollback rolls tx back after cause and keeps a rollback failure next to the cause. A
// transaction that is already done, as when its context was canceled, needs no rollback.
func rollback(tx *sqlx.Tx, cause error) error {
	err := tx.Rollback()
	if err == nil || errors.Is(err, sql.ErrTxDone) {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("rollback transaction: %w", err))
}

func txFrom(ctx context.Context) (*sqlx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(*sqlx.Tx)
	return tx, ok
}

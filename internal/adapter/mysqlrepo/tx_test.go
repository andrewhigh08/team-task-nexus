package mysqlrepo

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// newMockDB returns a sqlx handle on go-sqlmock that fails the test if an expected statement
// is not executed.
func newMockDB(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	return sqlx.NewDb(db, "mysql"), mock
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok, "%v is not an AppError", err)
	assert.Equal(t, code, appErr.Code)
}

func TestTxManager_WithinTx(t *testing.T) {
	t.Parallel()
	errFn := errors.New("fn failed")
	errDB := errors.New("connection lost")
	tests := []struct {
		name   string
		expect func(sqlmock.Sqlmock)
		fnErr  error
		check  func(t *testing.T, err error)
	}{
		{
			name:   "commits when fn succeeds",
			expect: func(m sqlmock.Sqlmock) { m.ExpectBegin(); m.ExpectCommit() },
			check:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:   "rolls back when fn fails",
			expect: func(m sqlmock.Sqlmock) { m.ExpectBegin(); m.ExpectRollback() },
			fnErr:  errFn,
			check:  func(t *testing.T, err error) { require.ErrorIs(t, err, errFn) },
		},
		{
			name:   "keeps a failed rollback next to the cause",
			expect: func(m sqlmock.Sqlmock) { m.ExpectBegin(); m.ExpectRollback().WillReturnError(errDB) },
			fnErr:  errFn,
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, errFn)
				require.ErrorIs(t, err, errDB)
			},
		},
		{
			name:   "failed begin",
			expect: func(m sqlmock.Sqlmock) { m.ExpectBegin().WillReturnError(errDB) },
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, errDB)
				requireCode(t, err, apperror.CodeInternal)
			},
		},
		{
			name:   "failed commit",
			expect: func(m sqlmock.Sqlmock) { m.ExpectBegin(); m.ExpectCommit().WillReturnError(errDB) },
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, errDB)
				requireCode(t, err, apperror.CodeInternal)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock := newMockDB(t)
			tt.expect(mock)

			err := NewTxManager(db).WithinTx(t.Context(), func(ctx context.Context) error {
				_, ok := txFrom(ctx)
				assert.True(t, ok, "fn runs with the transaction in its context")
				return tt.fnErr
			})

			tt.check(t, err)
		})
	}
}

func TestTxManager_WithinTx_RollsBackAndRepanics(t *testing.T) {
	t.Parallel()
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	assert.PanicsWithValue(t, "boom", func() {
		_ = NewTxManager(db).WithinTx(t.Context(), func(context.Context) error { panic("boom") })
	})
}

func TestTxManager_WithinTx_NestedCallJoinsTheOuterTransaction(t *testing.T) {
	t.Parallel()
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectCommit()
	txm := NewTxManager(db)

	err := txm.WithinTx(t.Context(), func(outer context.Context) error {
		outerTx, _ := txFrom(outer)
		return txm.WithinTx(outer, func(inner context.Context) error {
			innerTx, _ := txFrom(inner)
			assert.Same(t, outerTx, innerTx)
			return nil
		})
	})

	require.NoError(t, err)
}

package mysqlrepo

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestPinger_Ping(t *testing.T) {
	t.Parallel()
	errDown := errors.New("connection refused")
	tests := []struct {
		name    string
		pingErr error
	}{
		{name: "mysql answers"},
		{name: "mysql is down", pingErr: errDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectPing().WillReturnError(tt.pingErr)

			err = NewPinger(sqlx.NewDb(db, "mysql")).Ping(t.Context())

			require.NoError(t, mock.ExpectationsWereMet(), "the pool pings MySQL")
			if tt.pingErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.pingErr)
		})
	}
}

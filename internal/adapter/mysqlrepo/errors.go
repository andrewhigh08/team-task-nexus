package mysqlrepo

import (
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// erDupEntry is the MySQL error number of a unique key violation.
const erDupEntry = 1062

func isDuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == erDupEntry
}

// lookupError maps a failed single-row lookup: no row means the resource with the given id
// does not exist, anything else is an internal failure of op.
func lookupError(err error, op, resource string, id any) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.NotFound(resource, id)
	}
	return apperror.Internal(op, err)
}

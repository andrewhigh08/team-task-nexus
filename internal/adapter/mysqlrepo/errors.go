package mysqlrepo

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

// erDupEntry is the MySQL error number of a unique key violation.
const erDupEntry = 1062

func isDuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == erDupEntry
}

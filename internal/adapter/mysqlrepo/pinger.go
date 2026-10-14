package mysqlrepo

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// Pinger checks that MySQL answers, for the readiness probe.
type Pinger struct {
	db *sqlx.DB
}

// NewPinger returns a Pinger that checks the pool db.
func NewPinger(db *sqlx.DB) *Pinger {
	return &Pinger{db: db}
}

// Ping checks that a connection of the pool reaches MySQL.
func (p *Pinger) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

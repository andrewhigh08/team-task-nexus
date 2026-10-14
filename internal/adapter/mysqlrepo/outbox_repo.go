package mysqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/apperror"
)

// maxLastErrorChars is the length of the outbox.last_error column.
const maxLastErrorChars = 1024

// newOutboxRow carries the payload as a string rather than []byte, which keeps the INSERT
// working should the DSN ever enable interpolateParams: interpolated, a []byte argument becomes
// a _binary literal, from which MySQL refuses to build a JSON value.
type newOutboxRow struct {
	EventType string `db:"event_type"`
	Payload   string `db:"payload"`
}

type outboxRow struct {
	ID        int64     `db:"id"`
	EventType string    `db:"event_type"`
	Payload   []byte    `db:"payload"`
	Attempts  int       `db:"attempts"`
	CreatedAt time.Time `db:"created_at"`
}

// OutboxRepo stores the events of the transactional outbox. Lease and send times come from
// the database clock, which every replica shares.
type OutboxRepo struct {
	db *sqlx.DB
	tx *TxManager
}

// NewOutboxRepo returns an OutboxRepo on db.
func NewOutboxRepo(db *sqlx.DB) *OutboxRepo {
	return &OutboxRepo{db: db, tx: NewTxManager(db)}
}

// Add inserts events with a single multi-row INSERT into the transaction carried by ctx.
// Outside a transaction an event could be stored for a change that is rolled back, or lost
// for one that commits, so Add refuses to run there.
func (r *OutboxRepo) Add(ctx context.Context, events ...domain.NewEvent) error {
	tx, ok := txFrom(ctx)
	if !ok {
		return apperror.Internal("add outbox events", errors.New("events added outside a transaction"))
	}
	if len(events) == 0 {
		return nil
	}
	rows := make([]newOutboxRow, 0, len(events))
	for _, e := range events {
		rows = append(rows, newOutboxRow{EventType: string(e.Type), Payload: string(e.Payload)})
	}
	// sqlx expands the VALUES clause to one tuple per row. The times are set explicitly rather
	// than left to the column defaults, which follow the time zone of the session.
	const query = `INSERT INTO outbox (event_type, payload, created_at, next_attempt_at)
		VALUES (:event_type, :payload, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))`
	if _, err := tx.NamedExecContext(ctx, query, rows); err != nil {
		return apperror.Internal("add outbox events", err)
	}
	return nil
}

// Claim leases up to limit due events in a short transaction of its own. SKIP LOCKED lets
// concurrent dispatchers pass over the rows another one is claiming instead of waiting for
// them, and the lease, written before the commit, keeps those rows from being claimed again
// once the locks are gone.
//
// The transaction runs at READ COMMITTED. At REPEATABLE READ the locking scan would lock the
// gaps of the index it reads as well, and every INSERT into the outbox would wait for the
// claim to commit, in request transactions that hold the row lock of a task meanwhile.
func (r *OutboxRepo) Claim(ctx context.Context, limit int, lease time.Duration) ([]domain.OutboxEvent, error) {
	var rows []outboxRow
	opts := &sql.TxOptions{Isolation: sql.LevelReadCommitted}
	err := r.tx.withinTx(ctx, opts, func(ctx context.Context) error {
		q := getQuerier(ctx, r.db)
		err := q.SelectContext(ctx, &rows, `
			SELECT id, event_type, payload, attempts, created_at
			FROM outbox
			WHERE sent_at IS NULL AND failed_at IS NULL AND next_attempt_at <= UTC_TIMESTAMP(3)
			ORDER BY id
			LIMIT ?
			FOR UPDATE SKIP LOCKED`, limit,
		)
		if err != nil {
			return apperror.Internal("claim outbox events", err)
		}
		if len(rows) == 0 {
			return nil
		}
		return leaseEvents(ctx, q, rows, lease)
	})
	if err != nil {
		return nil, err
	}

	events := make([]domain.OutboxEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, domain.OutboxEvent{
			ID:        row.ID,
			Type:      domain.EventType(row.EventType),
			Payload:   row.Payload,
			Attempts:  row.Attempts + 1, // the attempt leaseEvents counted
			CreatedAt: row.CreatedAt,
		})
	}
	return events, nil
}

// leaseEvents counts a delivery attempt for each claimed row and holds the rows back until
// the lease ends.
func leaseEvents(ctx context.Context, q querier, rows []outboxRow, lease time.Duration) error {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	query, args, err := sqlx.In(`
		UPDATE outbox
		SET attempts = attempts + 1, next_attempt_at = UTC_TIMESTAMP(3) + INTERVAL ? MICROSECOND
		WHERE id IN (?)`, lease.Microseconds(), ids,
	)
	if err != nil {
		return apperror.Internal("lease outbox events", err)
	}
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return apperror.Internal("lease outbox events", err)
	}
	return nil
}

// Release takes back the attempt Claim counted for the events with the given ids and makes
// them due at once. Events recorded as sent or failed in the meantime are left alone.
func (r *OutboxRepo) Release(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	query, args, err := sqlx.In(`
		UPDATE outbox
		SET attempts = attempts - 1, next_attempt_at = UTC_TIMESTAMP(3)
		WHERE id IN (?) AND sent_at IS NULL AND failed_at IS NULL`, ids,
	)
	if err != nil {
		return apperror.Internal("release outbox events", err)
	}
	if _, err := getQuerier(ctx, r.db).ExecContext(ctx, query, args...); err != nil {
		return apperror.Internal("release outbox events", err)
	}
	return nil
}

// MarkSent records the delivery of the event with the given id.
func (r *OutboxRepo) MarkSent(ctx context.Context, id int64) error {
	return r.mark(ctx, "mark outbox event sent", id,
		"UPDATE outbox SET sent_at = UTC_TIMESTAMP(3) WHERE id = ?")
}

// MarkRetry records lastErr, made to fit its column, and makes the event due again at next.
func (r *OutboxRepo) MarkRetry(ctx context.Context, id int64, lastErr string, next time.Time) error {
	return r.mark(ctx, "mark outbox event for retry", id,
		"UPDATE outbox SET last_error = ?, next_attempt_at = ? WHERE id = ?", lastErrorText(lastErr), next.UTC())
}

// MarkFailed records lastErr, made to fit its column, and gives up on the event.
func (r *OutboxRepo) MarkFailed(ctx context.Context, id int64, lastErr string) error {
	return r.mark(ctx, "mark outbox event failed", id,
		"UPDATE outbox SET last_error = ?, failed_at = UTC_TIMESTAMP(3) WHERE id = ?", lastErrorText(lastErr))
}

// lastErrorText makes lastErr fit the last_error column. An error may quote what a remote
// server sent, such as the reason phrase of its status line; invalid UTF-8 in it is replaced,
// since MySQL in strict mode would refuse the whole UPDATE and the event would then be
// delivered again after every lease.
func lastErrorText(lastErr string) string {
	return truncate(strings.ToValidUTF8(lastErr, "\uFFFD"), maxLastErrorChars)
}

// mark runs an UPDATE of the event with the given id, which is the last argument of query.
func (r *OutboxRepo) mark(ctx context.Context, op string, id int64, query string, args ...any) error {
	result, err := getQuerier(ctx, r.db).ExecContext(ctx, query, append(args, id)...)
	if err != nil {
		return apperror.Internal(op, err)
	}
	found, err := affectedOne(result, op)
	if err != nil {
		return err
	}
	if !found {
		return apperror.NotFound("outbox event", id)
	}
	return nil
}

// truncate returns the first maxChars characters of s.
func truncate(s string, maxChars int) string {
	n := 0
	for i := range s {
		if n == maxChars {
			return s[:i]
		}
		n++
	}
	return s
}

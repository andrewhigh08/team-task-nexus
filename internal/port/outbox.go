package port

import (
	"context"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// OutboxRepository stores events in the database next to the changes they describe, until
// they are delivered. Events are written in the transaction of their change, so an event
// exists exactly when its change was committed; delivery happens later, at least once.
type OutboxRepository interface {
	// Add stores events; it must run inside TxManager.WithinTx.
	Add(ctx context.Context, events ...domain.NewEvent) error
	// Claim leases up to limit due events, oldest first, and counts a delivery attempt for
	// each. A leased event is not handed out again until lease has passed, so concurrent
	// dispatchers never claim the same event, while the events of a dispatcher that stopped
	// mid-delivery become due again.
	Claim(ctx context.Context, limit int, lease time.Duration) ([]domain.OutboxEvent, error)
	// Release hands back claimed events that were not delivered: it takes back the attempt
	// Claim counted for them and makes them due at once.
	Release(ctx context.Context, ids []int64) error
	// MarkSent records that the event was delivered; it is never handed out again.
	MarkSent(ctx context.Context, id int64) error
	// MarkRetry records the failed delivery attempt and makes the event due again at next.
	MarkRetry(ctx context.Context, id int64, lastErr string, next time.Time) error
	// MarkFailed records the last failed attempt and gives up on the event.
	MarkFailed(ctx context.Context, id int64, lastErr string) error
}

// EventSender delivers an event to the systems that consume it.
type EventSender interface {
	// Send delivers event once; an error means it has to be delivered again.
	Send(ctx context.Context, event domain.OutboxEvent) error
}

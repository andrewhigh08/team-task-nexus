package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/mysqlrepo"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

func TestAssignmentAndCommentEventsReachTheWebhook(t *testing.T) {
	t.Parallel()
	s := env(t)
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Events")
	dev := c.mustJoin(t, owner, team.ID, "Developer", "member")

	task := c.mustCreateTask(t, owner, team.ID, "Ship it", map[string]any{"assignee_id": dev.ID})
	c.mustPatchTask(t, dev, task.ID, map[string]any{"assignee_id": owner.ID})
	commentResp := c.do(t, http.MethodPost, taskPath(task.ID, "/comments"), map[string]string{"content": "Ready?"}, dev.Token)
	comment := expect[commentData](t, commentResp, http.StatusCreated)
	// Clearing an assignment notifies nobody.
	c.mustPatchTask(t, owner, task.ID, map[string]any{"assignee_id": nil})

	deliveries := s.webhook.awaitAccepted(t, team.ID, 3)
	require.Len(t, deliveries, 3)
	for _, d := range deliveries {
		assert.True(t, d.accepted())
		assert.Equal(t, "application/json", d.ContentType)
		assert.Equal(t, strconv.FormatInt(d.Event.ID, 10), d.EventID)
		assert.Equal(t, d.Event.Type, d.EventType)
		assert.WithinDuration(t, time.Now(), d.Event.OccurredAt, time.Minute)
	}
	// One dispatcher delivers the events in the order they were written.
	assert.Equal(t, "task.assigned", deliveries[0].EventType)
	assert.JSONEq(t, fmt.Sprintf(`{"task_id":%d,"team_id":%d,"title":"Ship it","assignee_id":%d,"actor_id":%d}`,
		task.ID, team.ID, dev.ID, owner.ID), string(deliveries[0].Event.Payload))
	assert.Equal(t, "task.assigned", deliveries[1].EventType)
	assert.JSONEq(t, fmt.Sprintf(`{"task_id":%d,"team_id":%d,"title":"Ship it","assignee_id":%d,"actor_id":%d}`,
		task.ID, team.ID, owner.ID, dev.ID), string(deliveries[1].Event.Payload))
	assert.Equal(t, "task.commented", deliveries[2].EventType)
	assert.JSONEq(t, fmt.Sprintf(`{"task_id":%d,"team_id":%d,"comment_id":%d,"author_id":%d,"task_creator_id":%d,"task_assignee_id":%d}`,
		task.ID, team.ID, comment.ID, dev.ID, owner.ID, owner.ID), string(deliveries[2].Event.Payload))

	// The outbox holds exactly these events, each marked as sent once the webhook took it.
	db := sharedDB(t)
	var counts eventCounts
	require.Eventually(t, func() bool {
		var err error
		counts, err = countTeamEvents(t.Context(), db, team.ID)
		return err == nil && counts.Sent == 3
	}, deliveryTimeout, deliveryPoll)
	assert.Equal(t, 3, counts.Total)
}

func TestRefusedDeliveriesAreRetriedWithBackoff(t *testing.T) {
	t.Parallel()
	s := env(t)
	c := newClient(t)
	owner := c.mustRegister(t, "Owner")
	team := c.mustCreateTeam(t, owner, "Flaky webhook")
	dev := c.mustJoin(t, owner, team.ID, "Developer", "member")

	s.webhook.refuse(team.ID, 2)
	task := c.mustCreateTask(t, owner, team.ID, "Retry me", map[string]any{"assignee_id": dev.ID})

	deliveries := s.webhook.awaitAccepted(t, team.ID, 1)
	require.Len(t, deliveries, 3)
	statuses := []int{deliveries[0].Status, deliveries[1].Status, deliveries[2].Status}
	assert.Equal(t, []int{http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusNoContent}, statuses)
	for _, d := range deliveries {
		assert.Equal(t, deliveries[0].Event, d.Event, "every attempt delivers the same event")
		assert.Equal(t, deliveries[0].EventID, d.EventID)
	}
	assert.JSONEq(t, fmt.Sprintf(`{"task_id":%d,"team_id":%d,"title":"Retry me","assignee_id":%d,"actor_id":%d}`,
		task.ID, team.ID, dev.ID, owner.ID), string(deliveries[0].Event.Payload))
	firstWait := deliveries[1].ReceivedAt.Sub(deliveries[0].ReceivedAt)
	secondWait := deliveries[2].ReceivedAt.Sub(deliveries[1].ReceivedAt)
	assert.Greater(t, secondWait, firstWait, "the delay between attempts grows")

	db := sharedDB(t)
	var row struct {
		Attempts  int     `db:"attempts"`
		Sent      bool    `db:"sent"`
		LastError *string `db:"last_error"`
	}
	require.Eventually(t, func() bool {
		err := db.GetContext(t.Context(), &row,
			"SELECT attempts, sent_at IS NOT NULL AS sent, last_error FROM outbox WHERE id = ?", deliveries[0].Event.ID)
		return err == nil && row.Sent
	}, deliveryTimeout, deliveryPoll)
	assert.Equal(t, 3, row.Attempts)
	require.NotNil(t, row.LastError)
	assert.Contains(t, *row.LastError, "webhook answered 503 Service Unavailable")
}

// eventCounts counts the outbox events of a team and those of them that were sent.
type eventCounts struct {
	Total int `db:"total"`
	Sent  int `db:"sent"`
}

// countTeamEvents runs inside require.Eventually, outside the test's goroutine, so it reports
// errors instead of failing the test.
func countTeamEvents(ctx context.Context, db *sqlx.DB, teamID int64) (eventCounts, error) {
	var counts eventCounts
	err := db.GetContext(ctx, &counts, `
		SELECT COUNT(*) AS total, COUNT(sent_at) AS sent
		FROM outbox
		WHERE JSON_EXTRACT(payload, '$.team_id') = ?`, teamID)
	return counts, err
}

// The tests below run the outbox repository on a database of their own, where no dispatcher
// of the shared application competes for the events.

func TestOutboxClaimsAreDisjointAcrossDispatchers(t *testing.T) {
	t.Parallel()
	db := openDB(t, migratedDatabase(t))
	outbox := mysqlrepo.NewOutboxRepo(db)
	ids := addEvents(t, db, 60)

	const dispatchers, batch = 2, 7
	claimed := make([][]int64, dispatchers)
	errs := make([]error, dispatchers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for d := range dispatchers {
		wg.Go(func() {
			<-start
			claimed[d], errs[d] = claimAll(t.Context(), outbox, batch)
		})
	}
	close(start)
	wg.Wait()
	require.NoError(t, errors.Join(errs...))

	all := slices.Concat(claimed...)
	slices.Sort(all)
	assert.Equal(t, ids, all, "every event is claimed by exactly one dispatcher")
}

// claimAll claims batches of up to limit events until no event is due, and returns the ids of
// the events it claimed. It runs outside the test's goroutine, so it reports errors instead of
// failing the test.
func claimAll(ctx context.Context, outbox *mysqlrepo.OutboxRepo, limit int) ([]int64, error) {
	var ids []int64
	for {
		events, err := outbox.Claim(ctx, limit, time.Minute)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			return ids, nil
		}
		ids = append(ids, eventIDs(events)...)
	}
}

func TestOutboxClaimSkipsEventsLockedByAnotherClaim(t *testing.T) {
	t.Parallel()
	db := openDB(t, migratedDatabase(t))
	outbox := mysqlrepo.NewOutboxRepo(db)
	ids := addEvents(t, db, 4)

	// Another dispatcher is in the middle of claiming the first two events and holds their
	// row locks. Its transaction runs at READ COMMITTED, as Claim's does; at REPEATABLE READ
	// the locking read would keep a lock on every row it scanned, not only on the two.
	tx, err := db.BeginTxx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)
	defer func() { assert.NoError(t, tx.Rollback()) }()
	var locked []int64
	require.NoError(t, tx.SelectContext(t.Context(), &locked,
		"SELECT id FROM outbox WHERE id IN (?, ?) FOR UPDATE", ids[0], ids[1]))
	require.Len(t, locked, 2)

	// Waiting for the locks would outlast the deadline; skipping them does not.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	events, err := outbox.Claim(ctx, 10, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, ids[2:], eventIDs(events))
}

func TestOutboxClaimsEventsInTheOrderTheyBecameDue(t *testing.T) {
	t.Parallel()
	db := openDB(t, migratedDatabase(t))
	outbox := mysqlrepo.NewOutboxRepo(db)
	ids := addEvents(t, db, 3)
	// The last event is made due an hour before the others, as a retry that came due before
	// they were written would be. The other two became due at the same time; the id decides.
	require.NoError(t, outbox.MarkRetry(t.Context(), ids[2], "webhook answered 503", time.Now().Add(-time.Hour)))

	events, err := outbox.Claim(t.Context(), 10, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, []int64{ids[2], ids[0], ids[1]}, eventIDs(events))
}

func TestOutboxLeaseExpiryMakesAnEventDueAgain(t *testing.T) {
	t.Parallel()
	db := openDB(t, migratedDatabase(t))
	outbox := mysqlrepo.NewOutboxRepo(db)
	ids := addEvents(t, db, 1)
	// Long enough for the check right after the claim to fall inside the lease on a loaded
	// machine, short enough not to slow the suite down.
	const lease = 3 * time.Second

	claimStarted := time.Now()
	first, err := outbox.Claim(t.Context(), 10, lease)
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, ids[0], first[0].ID)
	assert.Equal(t, 1, first[0].Attempts)

	// While the lease runs, the event is held back from every other claim.
	held, err := outbox.Claim(t.Context(), 10, lease)
	require.NoError(t, err)
	assert.Empty(t, held)

	// A dispatcher that stopped mid-delivery leaves the event to be claimed again once the
	// lease ends. Polling claims nothing until then, so it does not disturb what it observes;
	// the callback runs outside the test's goroutine and only records what it saw.
	var again []domain.OutboxEvent
	require.Eventually(t, func() bool {
		events, err := outbox.Claim(t.Context(), 10, lease)
		if err != nil || len(events) == 0 {
			return false
		}
		again = events
		return true
	}, lease+5*time.Second, 100*time.Millisecond, "the event never became due again")
	// The lease is measured by the database clock, which runs at the pace of ours, and it
	// started after claimStarted.
	assert.GreaterOrEqual(t, time.Since(claimStarted), lease, "the event was due again before its lease ended")
	require.Len(t, again, 1)
	assert.Equal(t, ids[0], again[0].ID)
	assert.Equal(t, 2, again[0].Attempts)
	assert.Equal(t, first[0].Payload, again[0].Payload)
	assert.Equal(t, first[0].CreatedAt, again[0].CreatedAt)
}

// addEvents writes n task.assigned events to the outbox in one transaction, as the services
// do, and returns their ids in the order they were written.
func addEvents(t *testing.T, db *sqlx.DB, n int) []int64 {
	t.Helper()
	events := make([]domain.NewEvent, 0, n)
	for i := range n {
		events = append(events, domain.NewEvent{Type: domain.EventTaskAssigned, Payload: fmt.Appendf(nil, `{"task_id":%d}`, i+1)})
	}
	outbox := mysqlrepo.NewOutboxRepo(db)
	err := mysqlrepo.NewTxManager(db).WithinTx(t.Context(), func(ctx context.Context) error {
		return outbox.Add(ctx, events...)
	})
	require.NoError(t, err)

	var ids []int64
	require.NoError(t, db.SelectContext(t.Context(), &ids, "SELECT id FROM outbox ORDER BY id"))
	require.Len(t, ids, n)
	return ids
}

func eventIDs(events []domain.OutboxEvent) []int64 {
	ids := make([]int64, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return ids
}

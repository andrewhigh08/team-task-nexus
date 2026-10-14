package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/metrics"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/test/mocks"
)

const (
	testBatchSize   = 50
	testLease       = 30 * time.Second
	testSendTimeout = 5 * time.Second
)

var errWebhookDown = errors.New("post event 5: webhook answered 503 Service Unavailable")

type dispatcherFixture struct {
	outbox *mocks.MockOutboxRepository
	sender *mocks.MockEventSender
}

func newDispatcherFixture(t *testing.T) *dispatcherFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	return &dispatcherFixture{outbox: mocks.NewMockOutboxRepository(ctrl), sender: mocks.NewMockEventSender(ctrl)}
}

func testDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		PollInterval: time.Millisecond,
		BatchSize:    testBatchSize,
		MaxAttempts:  12,
		Lease:        testLease,
		SendTimeout:  testSendTimeout,
		Clock:        fixedClock,
	}
}

func (f *dispatcherFixture) dispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	d, err := NewDispatcher(f.outbox, f.sender, testDispatcherConfig())
	require.NoError(t, err)
	return d
}

// expectClaim expects a claim of a batch with the configured size and lease, answering events.
func (f *dispatcherFixture) expectClaim(events ...domain.OutboxEvent) *gomock.Call {
	return f.outbox.EXPECT().Claim(gomock.Any(), testBatchSize, testLease).Return(events, nil)
}

// recordContext matches the context of a write to the outbox after a claim: one that a stop
// of Run does not cancel, but that ends within recordTimeout.
func recordContext() gomock.Matcher {
	return gomock.WantFormatter(
		gomock.StringerFunc(func() string { return "a live context ending within recordTimeout" }),
		gomock.Cond(func(ctx context.Context) bool {
			deadline, ok := ctx.Deadline()
			return ok && ctx.Err() == nil && time.Until(deadline) <= recordTimeout
		}),
	)
}

func outboxEvent(id int64, typ domain.EventType, attempts int) domain.OutboxEvent {
	return domain.OutboxEvent{
		ID:        id,
		Type:      typ,
		Payload:   []byte(`{"task_id": 11}`),
		Attempts:  attempts,
		CreatedAt: testNow.Add(-time.Minute),
	}
}

func TestNewDispatcher_RejectsInvalidSetup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		noOutbox  bool
		noSender  bool
		mutateCfg func(*DispatcherConfig)
		want      string
	}{
		{name: "outbox", noOutbox: true, want: "outbox repository is required"},
		{name: "sender", noSender: true, want: "event sender is required"},
		{name: "poll interval", mutateCfg: func(c *DispatcherConfig) { c.PollInterval = 0 }, want: "poll interval must be positive"},
		{name: "batch size", mutateCfg: func(c *DispatcherConfig) { c.BatchSize = 0 }, want: "batch size must be positive"},
		{name: "max attempts", mutateCfg: func(c *DispatcherConfig) { c.MaxAttempts = -1 }, want: "max attempts must be positive"},
		{name: "send timeout", mutateCfg: func(c *DispatcherConfig) { c.SendTimeout = 0 }, want: "send timeout must be positive"},
		{name: "lease", mutateCfg: func(c *DispatcherConfig) { c.Lease = testSendTimeout }, want: "lease must be longer than the send timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newDispatcherFixture(t)
			var (
				outbox port.OutboxRepository = f.outbox
				sender port.EventSender      = f.sender
				cfg                          = testDispatcherConfig()
			)
			if tt.noOutbox {
				outbox = nil
			}
			if tt.noSender {
				sender = nil
			}
			if tt.mutateCfg != nil {
				tt.mutateCfg(&cfg)
			}

			d, err := NewDispatcher(outbox, sender, cfg)

			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, d)
		})
	}
}

func TestDispatcher_RecordsTheOutcomeOfADelivery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		attempts int
		sendErr  error
		expect   func(outbox *mocks.MockOutboxRepository)
	}{
		{
			name: "delivered", attempts: 1,
			expect: func(o *mocks.MockOutboxRepository) { o.EXPECT().MarkSent(recordContext(), int64(5)).Return(nil) },
		},
		{
			name: "first failure is retried after a second", attempts: 1, sendErr: errWebhookDown,
			expect: func(o *mocks.MockOutboxRepository) {
				o.EXPECT().MarkRetry(recordContext(), int64(5), errWebhookDown.Error(), testNow.Add(time.Second)).Return(nil)
			},
		},
		{
			name: "third failure is retried after four seconds", attempts: 3, sendErr: errWebhookDown,
			expect: func(o *mocks.MockOutboxRepository) {
				o.EXPECT().MarkRetry(recordContext(), int64(5), errWebhookDown.Error(), testNow.Add(4*time.Second)).Return(nil)
			},
		},
		{
			name: "the delay stops growing at five minutes", attempts: 11, sendErr: errWebhookDown,
			expect: func(o *mocks.MockOutboxRepository) {
				o.EXPECT().MarkRetry(recordContext(), int64(5), errWebhookDown.Error(), testNow.Add(5*time.Minute)).Return(nil)
			},
		},
		{
			name: "failure of the last attempt gives up", attempts: 12, sendErr: errWebhookDown,
			expect: func(o *mocks.MockOutboxRepository) {
				o.EXPECT().MarkFailed(recordContext(), int64(5), errWebhookDown.Error()).Return(nil)
			},
		},
		{
			name: "attempts beyond a lowered maximum give up", attempts: 20, sendErr: errWebhookDown,
			expect: func(o *mocks.MockOutboxRepository) {
				o.EXPECT().MarkFailed(recordContext(), int64(5), errWebhookDown.Error()).Return(nil)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newDispatcherFixture(t)
			event := outboxEvent(5, domain.EventTaskAssigned, tt.attempts)
			f.expectClaim(event)
			f.sender.EXPECT().Send(gomock.Any(), event).Return(tt.sendErr)
			tt.expect(f.outbox)
			ctx, _ := captureLog(t)

			f.dispatcher(t).dispatch(ctx)
		})
	}
}

// TestDispatcher_DeliversTheWholeBatch uses an event type of its own, so the counters it
// checks are not touched by the other tests running in parallel.
func TestDispatcher_DeliversTheWholeBatch(t *testing.T) {
	t.Parallel()
	const eventType domain.EventType = "test.whole_batch"
	f := newDispatcherFixture(t)
	sent, retried, failed, unrecorded := outboxEvent(1, eventType, 1), outboxEvent(2, eventType, 2),
		outboxEvent(3, eventType, 12), outboxEvent(4, eventType, 1)
	f.expectClaim(sent, retried, failed, unrecorded)
	gomock.InOrder(
		f.sender.EXPECT().Send(gomock.Any(), sent).Return(nil),
		f.outbox.EXPECT().MarkSent(gomock.Any(), sent.ID).Return(nil),
		f.sender.EXPECT().Send(gomock.Any(), retried).Return(errWebhookDown),
		f.outbox.EXPECT().MarkRetry(gomock.Any(), retried.ID, errWebhookDown.Error(), testNow.Add(2*time.Second)).Return(nil),
		f.sender.EXPECT().Send(gomock.Any(), failed).Return(errWebhookDown),
		f.outbox.EXPECT().MarkFailed(gomock.Any(), failed.ID, errWebhookDown.Error()).Return(nil),
		f.sender.EXPECT().Send(gomock.Any(), unrecorded).Return(nil),
		f.outbox.EXPECT().MarkSent(gomock.Any(), unrecorded.ID).Return(errors.New("connection reset by peer")),
	)
	ctx, logs := captureLog(t)
	before := outboxCounts(eventType)

	f.dispatcher(t).dispatch(ctx)

	assert.Equal(t, map[string]float64{
		metrics.ResultSent:   before[metrics.ResultSent] + 2,
		metrics.ResultRetry:  before[metrics.ResultRetry] + 1,
		metrics.ResultFailed: before[metrics.ResultFailed] + 1,
	}, outboxCounts(eventType))
	assert.Equal(t, []string{
		"WARN event delivery failed, will retry",
		"ERROR event delivery failed, giving up",
		"ERROR record event delivery failed",
	}, logMessages(t, logs))
}

func TestDispatcher_BoundsEveryDeliveryBySendTimeout(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t)
	event := outboxEvent(1, domain.EventTaskAssigned, 1)
	f.expectClaim(event)
	f.sender.EXPECT().Send(gomock.Any(), event).DoAndReturn(func(ctx context.Context, _ domain.OutboxEvent) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok, "the delivery has a deadline")
		assert.WithinDuration(t, time.Now().Add(testSendTimeout), deadline, time.Second)
		return nil
	})
	f.outbox.EXPECT().MarkSent(gomock.Any(), event.ID).DoAndReturn(func(ctx context.Context, _ int64) error {
		assert.NoError(t, ctx.Err(), "the outcome is recorded once the delivery is over")
		return nil
	})
	ctx, _ := captureLog(t)

	f.dispatcher(t).dispatch(ctx)
}

// slowBatch returns a dispatcher whose clock advances by sendTime with every delivery, as with
// a webhook that hangs, after it claimed a batch of size events with ids from 1. It expects the
// first delivered events of the batch to be sent and marked sent, in order, and returns the
// last of those calls.
func (f *dispatcherFixture) slowBatch(t *testing.T, size, delivered int, sendTime time.Duration) (*Dispatcher, *gomock.Call) {
	t.Helper()
	now := testNow
	cfg := testDispatcherConfig()
	cfg.Clock = func() time.Time { return now }
	d, err := NewDispatcher(f.outbox, f.sender, cfg)
	require.NoError(t, err)
	batch := make([]domain.OutboxEvent, 0, size)
	for id := int64(1); id <= int64(size); id++ {
		batch = append(batch, outboxEvent(id, domain.EventTaskAssigned, 1))
	}
	calls := make([]any, 0, 1+2*delivered)
	calls = append(calls, f.expectClaim(batch...))
	for _, event := range batch[:delivered] {
		calls = append(calls,
			f.sender.EXPECT().Send(gomock.Any(), event).DoAndReturn(func(context.Context, domain.OutboxEvent) error {
				now = now.Add(sendTime)
				return nil
			}),
			f.outbox.EXPECT().MarkSent(gomock.Any(), event.ID).Return(nil),
		)
	}
	gomock.InOrder(calls...)
	return d, calls[len(calls)-1].(*gomock.Call)
}

// TestDispatcher_ReleasesWhatTheLeaseWouldNotOutlast lets every delivery take the whole send
// timeout: the 30s lease of the batch covers five of them.
func TestDispatcher_ReleasesWhatTheLeaseWouldNotOutlast(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t)
	d, lastSent := f.slowBatch(t, 7, 5, testSendTimeout)
	// The sixth delivery would start 25s into the lease and could outlast it: the rest of the
	// batch is handed back without the attempt the claim counted.
	f.outbox.EXPECT().Release(recordContext(), []int64{6, 7}).After(lastSent).Return(nil)
	ctx, logs := captureLog(t)

	d.dispatch(ctx)

	assert.Empty(t, logMessages(t, logs))
}

func TestDispatcher_LogsAFailedRelease(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t)
	d, lastSent := f.slowBatch(t, 2, 1, testLease-testSendTimeout)
	f.outbox.EXPECT().Release(gomock.Any(), []int64{2}).After(lastSent).Return(errors.New("connection reset by peer"))
	ctx, logs := captureLog(t)

	d.dispatch(ctx)

	assert.Equal(t, []string{"ERROR release outbox events failed"}, logMessages(t, logs))
}

// TestDispatcher_ReleasesNothingOnceTheLeaseHasEnded has a delivery outlast the lease, as a
// slow write of its outcome could.
func TestDispatcher_ReleasesNothingOnceTheLeaseHasEnded(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t)
	d, _ := f.slowBatch(t, 2, 1, testLease)
	// No Release of the second event: another dispatcher may have claimed it already.
	ctx, _ := captureLog(t)

	d.dispatch(ctx)
}

// logMessages returns the level and message of every JSON log record in logs, in order.
func logMessages(t *testing.T, logs *bytes.Buffer) []string {
	t.Helper()
	var messages []string
	dec := json.NewDecoder(logs)
	for {
		var rec map[string]any
		err := dec.Decode(&rec)
		if errors.Is(err, io.EOF) {
			return messages
		}
		require.NoError(t, err)
		messages = append(messages, fmt.Sprintf("%s %s", rec["level"], rec["msg"]))
	}
}

func outboxCounts(eventType domain.EventType) map[string]float64 {
	counts := make(map[string]float64)
	for _, result := range []string{metrics.ResultSent, metrics.ResultRetry, metrics.ResultFailed} {
		counts[result] = testutil.ToFloat64(metrics.OutboxEvents.WithLabelValues(string(eventType), result))
	}
	return counts
}

// runDispatcher runs d until ctx is canceled and fails the test if Run does not return soon
// after.
func runDispatcher(t *testing.T, ctx context.Context, d *Dispatcher) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was canceled")
	}
}

// TestDispatcher_Run_FinishesTheDeliveryInFlight stops the dispatcher while it sends the first
// event of a batch, as a SIGTERM would.
func TestDispatcher_Run_FinishesTheDeliveryInFlight(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t)
	logCtx, _ := captureLog(t)
	ctx, stop := context.WithCancel(logCtx)
	defer stop()
	inFlight, next, last := outboxEvent(1, domain.EventTaskAssigned, 1), outboxEvent(2, domain.EventTaskAssigned, 1),
		outboxEvent(3, domain.EventTaskCommented, 1)
	f.expectClaim(inFlight, next, last)
	gomock.InOrder(
		f.sender.EXPECT().Send(gomock.Any(), inFlight).DoAndReturn(func(ctx context.Context, _ domain.OutboxEvent) error {
			stop()
			assert.NoError(t, ctx.Err(), "the delivery in flight is not cut short")
			return nil
		}),
		// Its outcome is recorded, and the rest of the batch is not delivered but handed back,
		// due at once for another replica. The stop cuts neither write short, and a deadline of
		// their own keeps a stalled database from holding up the shutdown.
		f.outbox.EXPECT().MarkSent(recordContext(), inFlight.ID).Return(nil),
		f.outbox.EXPECT().Release(recordContext(), []int64{next.ID, last.ID}).Return(nil),
	)

	runDispatcher(t, ctx, f.dispatcher(t))
}

func TestDispatcher_Run_RetriesAFailedClaimOnTheNextTick(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t)
	logCtx, logs := captureLog(t)
	ctx, stop := context.WithCancel(logCtx)
	defer stop()
	event := outboxEvent(1, domain.EventTaskCommented, 1)
	gomock.InOrder(
		f.outbox.EXPECT().Claim(gomock.Any(), testBatchSize, testLease).Return(nil, errors.New("too many connections")),
		f.expectClaim(),
		f.expectClaim(event),
		f.sender.EXPECT().Send(gomock.Any(), event).Return(nil),
		f.outbox.EXPECT().MarkSent(gomock.Any(), event.ID).DoAndReturn(func(context.Context, int64) error {
			stop()
			return nil
		}),
	)

	runDispatcher(t, ctx, f.dispatcher(t))

	assert.Equal(t, []string{"ERROR claim outbox events failed"}, logMessages(t, logs))
}

func TestDispatcher_Run_StaysQuietWhenStoppedDuringAClaim(t *testing.T) {
	t.Parallel()
	f := newDispatcherFixture(t)
	logCtx, logs := captureLog(t)
	ctx, stop := context.WithCancel(logCtx)
	defer stop()
	f.outbox.EXPECT().Claim(gomock.Any(), testBatchSize, testLease).DoAndReturn(
		func(ctx context.Context, _ int, _ time.Duration) ([]domain.OutboxEvent, error) {
			stop()
			return nil, ctx.Err()
		})

	runDispatcher(t, ctx, f.dispatcher(t))

	assert.Empty(t, logMessages(t, logs), "a claim interrupted by shutdown is not an error")
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{attempts: 1, want: time.Second},
		{attempts: 2, want: 2 * time.Second},
		{attempts: 3, want: 4 * time.Second},
		{attempts: 9, want: 256 * time.Second},
		{attempts: 10, want: 5 * time.Minute},
		{attempts: 64, want: 5 * time.Minute},
		{attempts: 1 << 30, want: 5 * time.Minute},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, backoff(tt.attempts), "after %d attempts", tt.attempts)
	}
}

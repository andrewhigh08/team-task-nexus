package service

import (
	"context"
	"errors"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/metrics"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// Delays between the delivery attempts of an event: they double from firstRetryDelay with
// every failed attempt, up to maxRetryDelay.
const (
	firstRetryDelay = time.Second
	maxRetryDelay   = 5 * time.Minute
)

// recordTimeout bounds every write to the outbox after a claim: the outcome of a delivery and
// the release of the rest of a batch. Those writes outlive the cancellation of Run, and without
// a deadline of their own a stalled database would hold up the shutdown for as long as the I/O
// timeouts of the driver allow. A write cut short leaves the event to its lease, as a failed
// one does.
const recordTimeout = 5 * time.Second

// DispatcherConfig tunes Dispatcher. SendTimeout bounds every delivery. A delivery starts only
// while the lease of its batch outlasts it, since once the lease expires another replica may
// claim the event and deliver it a second time while it is still in flight; Lease must
// therefore exceed SendTimeout. A nil Clock means time.Now.
type DispatcherConfig struct {
	PollInterval time.Duration
	BatchSize    int
	MaxAttempts  int
	Lease        time.Duration
	SendTimeout  time.Duration
	Clock        func() time.Time
}

func (c DispatcherConfig) validate() error {
	switch {
	case c.PollInterval <= 0:
		return errors.New("dispatcher: poll interval must be positive")
	case c.BatchSize <= 0:
		return errors.New("dispatcher: batch size must be positive")
	case c.MaxAttempts <= 0:
		return errors.New("dispatcher: max attempts must be positive")
	case c.SendTimeout <= 0:
		return errors.New("dispatcher: send timeout must be positive")
	case c.Lease <= c.SendTimeout:
		return errors.New("dispatcher: lease must be longer than the send timeout")
	}
	return nil
}

// Dispatcher delivers the events of the outbox. Every poll interval it claims a batch of due
// events and sends them one by one: a delivered event is marked sent, a failed one is retried
// with exponential backoff until it has had MaxAttempts deliveries, then given up on. Events
// are delivered at least once, since a dispatcher that stops between sending an event and
// marking it leaves the event to be claimed again once its lease expires.
type Dispatcher struct {
	outbox port.OutboxRepository
	sender port.EventSender
	cfg    DispatcherConfig
}

// NewDispatcher validates cfg and returns a Dispatcher delivering the events of outbox
// through sender.
func NewDispatcher(outbox port.OutboxRepository, sender port.EventSender, cfg DispatcherConfig) (*Dispatcher, error) {
	switch {
	case outbox == nil:
		return nil, errors.New("dispatcher: outbox repository is required")
	case sender == nil:
		return nil, errors.New("dispatcher: event sender is required")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &Dispatcher{outbox: outbox, sender: sender, cfg: cfg}, nil
}

// Run dispatches events until ctx is canceled, then returns nil. It logs through the logger
// carried by ctx.
func (d *Dispatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()
	// The loop condition, not the select, ends Run: a select with a tick pending as well picks
	// either case, and would otherwise start one more dispatch after ctx is canceled.
	for ctx.Err() == nil {
		d.dispatch(ctx)
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil
}

// dispatch claims one batch and delivers it. A failed claim is retried on the next tick.
// Deliveries stop once ctx is canceled or the lease of the batch would not outlast the next
// one, and the rest of the batch is released. The delivery in flight at cancellation runs to
// its end, bounded by SendTimeout, so that its outcome is recorded instead of being left to
// the lease; the writes that follow are bounded by recordTimeout.
func (d *Dispatcher) dispatch(ctx context.Context) {
	// Taken before the claim, so the lease written by the claim lasts at least until then.
	leaseEnd := d.cfg.Clock().Add(d.cfg.Lease)
	events, err := d.outbox.Claim(ctx, d.cfg.BatchSize, d.cfg.Lease)
	if err != nil {
		if ctx.Err() == nil {
			logger.FromContext(ctx).Error("claim outbox events failed", "error", err)
		}
		return
	}
	uncanceled := context.WithoutCancel(ctx)
	for i, event := range events {
		if ctx.Err() != nil || !d.cfg.Clock().Add(d.cfg.SendTimeout).Before(leaseEnd) {
			d.release(uncanceled, events[i:], leaseEnd)
			return
		}
		d.deliver(uncanceled, event)
	}
}

// release hands back events claimed but not delivered, so that the attempt counted by the
// claim does not bring them closer to being given up on. Once the lease has ended another
// dispatcher may hold the events already, and releasing them would take back its attempt and
// its lease; they are left as they are then.
func (d *Dispatcher) release(ctx context.Context, events []domain.OutboxEvent, leaseEnd time.Time) {
	if !d.cfg.Clock().Before(leaseEnd) {
		return
	}
	ids := make([]int64, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	ctx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()
	if err := d.outbox.Release(ctx, ids); err != nil {
		// They become due again when the lease ends, with the attempt counted.
		logger.FromContext(ctx).Error("release outbox events failed", "error", err)
	}
}

// deliver sends event and records the outcome. A failed send is recorded for a retry, or as
// the end of the event once it has had MaxAttempts deliveries.
func (d *Dispatcher) deliver(ctx context.Context, event domain.OutboxEvent) {
	log := logger.FromContext(ctx).With("event_id", event.ID, "event_type", string(event.Type), "attempt", event.Attempts)
	sendCtx, cancelSend := context.WithTimeout(ctx, d.cfg.SendTimeout)
	sendErr := d.sender.Send(sendCtx, event)
	cancelSend()

	ctx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()
	var result string
	var err error
	switch {
	case sendErr == nil:
		result = metrics.ResultSent
		err = d.outbox.MarkSent(ctx, event.ID)
	case event.Attempts >= d.cfg.MaxAttempts:
		result = metrics.ResultFailed
		log.Error("event delivery failed, giving up", "error", sendErr)
		err = d.outbox.MarkFailed(ctx, event.ID, sendErr.Error())
	default:
		result = metrics.ResultRetry
		next := d.cfg.Clock().Add(backoff(event.Attempts))
		log.Warn("event delivery failed, will retry", "error", sendErr, "next_attempt_at", next)
		err = d.outbox.MarkRetry(ctx, event.ID, sendErr.Error(), next)
	}
	metrics.RecordOutboxEvent(string(event.Type), result)
	if err != nil {
		// The lease expires and the event is delivered again.
		log.Error("record event delivery failed", "result", result, "error", err)
	}
}

// backoff returns the delay after the given number of failed delivery attempts.
func backoff(attempts int) time.Duration {
	delay := firstRetryDelay
	for i := 1; i < attempts && delay < maxRetryDelay; i++ {
		delay *= 2
	}
	return min(delay, maxRetryDelay)
}

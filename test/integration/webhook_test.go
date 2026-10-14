package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Bounds of the wait for the dispatcher. A delivery refused twice is retried after one and
// then two more seconds, so the deadline leaves room for a few retries.
const (
	deliveryTimeout = 20 * time.Second
	deliveryPoll    = 50 * time.Millisecond
)

// webhookEvent is the body the application POSTs for an event.
type webhookEvent struct {
	ID         int64           `json:"id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

// delivery is one request the webhook received and what it answered.
type delivery struct {
	ContentType string
	EventID     string // X-Event-ID header
	EventType   string // X-Event-Type header
	Event       webhookEvent
	TeamID      int64
	Status      int
	ReceivedAt  time.Time
}

func (d delivery) accepted() bool {
	return d.Status == http.StatusNoContent
}

// webhookRecorder receives the events the application under test delivers. It records every
// delivery and can refuse the next deliveries of a team's events, so a test can watch the
// dispatcher retry. Events are told apart by team, since every test works in teams of its own.
type webhookRecorder struct {
	server *httptest.Server

	mu         sync.Mutex
	deliveries []delivery
	refusals   map[int64]int // team id -> deliveries still to refuse
}

func newWebhookRecorder() *webhookRecorder {
	w := &webhookRecorder{refusals: make(map[int64]int)}
	w.server = httptest.NewServer(http.HandlerFunc(w.receive))
	return w
}

func (w *webhookRecorder) URL() string {
	return w.server.URL
}

func (w *webhookRecorder) Close() {
	w.server.Close()
}

// receive answers 204 to a well-formed event, 503 to one whose team has refusals left and 400
// to a body it cannot read; the dispatcher retries every answer but the 204.
func (w *webhookRecorder) receive(rw http.ResponseWriter, r *http.Request) {
	d := delivery{
		ContentType: r.Header.Get("Content-Type"),
		EventID:     r.Header.Get("X-Event-ID"),
		EventType:   r.Header.Get("X-Event-Type"),
		Status:      http.StatusNoContent,
		ReceivedAt:  time.Now(),
	}
	if err := decodeEvent(r.Body, &d); err != nil {
		d.Status = http.StatusBadRequest
	}

	w.mu.Lock()
	if d.Status == http.StatusNoContent && w.refusals[d.TeamID] > 0 {
		w.refusals[d.TeamID]--
		d.Status = http.StatusServiceUnavailable
	}
	w.deliveries = append(w.deliveries, d)
	w.mu.Unlock()

	rw.WriteHeader(d.Status)
}

func decodeEvent(body io.Reader, d *delivery) error {
	if err := json.NewDecoder(body).Decode(&d.Event); err != nil {
		return err
	}
	var payload struct {
		TeamID int64 `json:"team_id"`
	}
	if err := json.Unmarshal(d.Event.Payload, &payload); err != nil {
		return err
	}
	d.TeamID = payload.TeamID
	return nil
}

// refuse makes the webhook answer 503 to the next n deliveries of the team's events.
func (w *webhookRecorder) refuse(teamID int64, n int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refusals[teamID] = n
}

// ofTeam returns the deliveries of the team's events in the order they arrived.
func (w *webhookRecorder) ofTeam(teamID int64) []delivery {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []delivery
	for _, d := range w.deliveries {
		if d.TeamID == teamID {
			out = append(out, d)
		}
	}
	return out
}

// awaitAccepted waits until the webhook has accepted n events of the team and returns every
// delivery of the team's events, refused ones included.
func (w *webhookRecorder) awaitAccepted(t *testing.T, teamID int64, n int) []delivery {
	t.Helper()
	require.Eventuallyf(t, func() bool { return countAccepted(w.ofTeam(teamID)) >= n },
		deliveryTimeout, deliveryPoll, "the webhook never accepted %d events of team %d", n, teamID)
	return w.ofTeam(teamID)
}

func countAccepted(deliveries []delivery) int {
	n := 0
	for _, d := range deliveries {
		if d.accepted() {
			n++
		}
	}
	return n
}

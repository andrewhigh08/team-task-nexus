package notify_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/notify"
	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// secretPath stands for the token webhook URLs often carry; it must never appear in errors.
const secretPath = "/hooks/s3cr3t-t0ken"

func testEvent() domain.OutboxEvent {
	return domain.OutboxEvent{
		ID:       42,
		Type:     domain.EventTaskAssigned,
		Payload:  []byte(`{"actor_id": 3, "task_id": 11, "team_id": 7, "title": "Rotate the certificates", "assignee_id": 2}`),
		Attempts: 2,
		// Stored in UTC; another zone checks that the document is in UTC all the same.
		CreatedAt: time.Date(2026, 3, 14, 17, 26, 53, 123_000_000, time.FixedZone("UTC+8", 8*60*60)),
	}
}

// request is what the test webhook received.
type request struct {
	method string
	path   string
	header http.Header
	body   string
}

// newWebhook starts a webhook answering status and returns its URL and the requests it got.
// Its answers point elsewhere with a Location header, and the test fails if the sender
// follows them there.
func newWebhook(t *testing.T, status int) (string, <-chan request) {
	t.Helper()
	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("the sender followed the webhook's answer: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(elsewhere.Close)
	requests := make(chan request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		requests <- request{method: r.Method, path: r.URL.Path, header: r.Header, body: string(body)}
		w.Header().Set("Location", elsewhere.URL+"/moved")
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + secretPath, requests
}

func TestWebhookSender_PostsTheEvent(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			url, requests := newWebhook(t, status)

			err := notify.NewWebhookSender(url, time.Second).Send(t.Context(), testEvent())

			require.NoError(t, err)
			got := <-requests
			assert.Equal(t, http.MethodPost, got.method)
			assert.Equal(t, secretPath, got.path)
			assert.Equal(t, "application/json", got.header.Get("Content-Type"))
			assert.Equal(t, "42", got.header.Get("X-Event-ID"))
			assert.Equal(t, "task.assigned", got.header.Get("X-Event-Type"))
			assert.JSONEq(t, `{
				"id": 42,
				"type": "task.assigned",
				"occurred_at": "2026-03-14T09:26:53.123Z",
				"payload": {"task_id": 11, "team_id": 7, "title": "Rotate the certificates", "assignee_id": 2, "actor_id": 3}
			}`, got.body)
		})
	}
}

func TestWebhookSender_FailsUnlessTheWebhookAccepts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status int
		want   string
	}{
		{status: http.StatusMovedPermanently, want: "post event 42: webhook answered 301 Moved Permanently"},
		{status: http.StatusFound, want: "post event 42: webhook answered 302 Found"},
		{status: http.StatusTemporaryRedirect, want: "post event 42: webhook answered 307 Temporary Redirect"},
		{status: http.StatusPermanentRedirect, want: "post event 42: webhook answered 308 Permanent Redirect"},
		{status: http.StatusBadRequest, want: "post event 42: webhook answered 400 Bad Request"},
		{status: http.StatusNotFound, want: "post event 42: webhook answered 404 Not Found"},
		{status: http.StatusInternalServerError, want: "post event 42: webhook answered 500 Internal Server Error"},
		{status: http.StatusServiceUnavailable, want: "post event 42: webhook answered 503 Service Unavailable"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()
			url, requests := newWebhook(t, tt.status)

			err := notify.NewWebhookSender(url, time.Second).Send(t.Context(), testEvent())

			require.EqualError(t, err, tt.want)
			<-requests
		})
	}
}

func TestWebhookSender_GivesUpAfterTheTimeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release // answers only once the test is over
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs first, so Close does not wait for the handler

	start := time.Now()
	err := notify.NewWebhookSender(srv.URL+secretPath, 50*time.Millisecond).Send(t.Context(), testEvent())

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.NotContains(t, err.Error(), secretPath)
}

func TestWebhookSender_KeepsTheURLOutOfErrors(t *testing.T) {
	t.Parallel()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	err = notify.NewWebhookSender("http://"+addr+secretPath, time.Second).Send(t.Context(), testEvent())

	require.Error(t, err, "nothing listens on the address")
	assert.Contains(t, err.Error(), "post event 42")
	assert.NotContains(t, err.Error(), secretPath)
}

// TestWebhookSender_ReusesTheConnection checks that response bodies are read to the end and
// closed, those of rejected deliveries included, so the connection can serve the next one.
// The client offers a connection back to its pool only once the response body was read to
// the end; the offer may still be refused when another test closes the shared idle
// connections, which says nothing about the sender.
func TestWebhookSender_ReusesTheConnection(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Event-ID") == "1" {
			w.WriteHeader(http.StatusInternalServerError)
		}
		_, _ = io.WriteString(w, strings.Repeat("a response nobody reads ", 1000))
	}))
	t.Cleanup(srv.Close)
	sender := notify.NewWebhookSender(srv.URL, time.Second)
	offered := make(chan struct{}, 2)
	ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
		PutIdleConn: func(error) { offered <- struct{}{} },
	})

	for id := int64(1); id <= 2; id++ {
		event := testEvent()
		event.ID = id
		_ = sender.Send(ctx, event)

		select {
		case <-offered:
		case <-time.After(5 * time.Second):
			t.Fatalf("event %d: the connection was not offered back to the pool", id)
		}
	}
}

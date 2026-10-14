package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// maxDrainedBytes bounds how much of a response body is read to keep its connection
// reusable; a longer body is cheaper to abandon with its connection.
const maxDrainedBytes = 64 << 10

// webhookBody is the JSON document POSTed for an event. Consumers deduplicate by id, since an
// event may be delivered more than once.
type webhookBody struct {
	ID         int64           `json:"id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

// WebhookSender delivers events by POSTing them to a URL. Any answer outside 2xx counts as a
// failed delivery, redirects included.
type WebhookSender struct {
	url    string
	client *http.Client
}

// NewWebhookSender returns a WebhookSender posting to rawURL, each delivery bounded by
// timeout.
func NewWebhookSender(rawURL string, timeout time.Duration) *WebhookSender {
	return &WebhookSender{url: rawURL, client: &http.Client{
		Timeout: timeout,
		// A followed 301, 302 or 303 turns the POST into a GET without the event, whose 2xx
		// would pass for a delivery, and a 307 or 308 sends the event wherever the answer
		// points; the redirect is reported as a failure instead.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Send POSTs event and reports whether the webhook accepted it. Errors never contain the URL,
// which may carry a secret token: they end up in the log and in the outbox.
func (s *WebhookSender) Send(ctx context.Context, event domain.OutboxEvent) error {
	body, err := json.Marshal(webhookBody{
		ID:         event.ID,
		Type:       string(event.Type),
		OccurredAt: event.CreatedAt.UTC(),
		Payload:    event.Payload,
	})
	if err != nil {
		return fmt.Errorf("encode event %d: %w", event.ID, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", withoutURL(err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-ID", strconv.FormatInt(event.ID, 10))
	req.Header.Set("X-Event-Type", string(event.Type))

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post event %d: %w", event.ID, withoutURL(err))
	}
	defer func() {
		// The rest of the body is read so the connection can serve the next delivery.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainedBytes))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("post event %d: webhook answered %s", event.ID, resp.Status)
	}
	return nil
}

// withoutURL strips the *url.Error wrapper the HTTP client puts around its errors, which
// quotes the URL.
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

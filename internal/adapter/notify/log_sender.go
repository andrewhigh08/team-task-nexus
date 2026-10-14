package notify

import (
	"context"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// LogSender delivers events to the log, for development and for deployments without a
// consumer. Payloads hold ids and task titles, never personal data such as emails.
type LogSender struct {
	log *logger.Logger
}

// NewLogSender returns a LogSender writing to log.
func NewLogSender(log *logger.Logger) *LogSender {
	return &LogSender{log: log}
}

// Send logs event; it never fails.
func (s *LogSender) Send(ctx context.Context, event domain.OutboxEvent) error {
	s.log.InfoContext(ctx, "event delivered",
		"event_id", event.ID,
		"event_type", string(event.Type),
		"attempt", event.Attempts,
		"payload", string(event.Payload),
	)
	return nil
}

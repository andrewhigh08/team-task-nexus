package app

import (
	"context"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/mysqlrepo"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/notify"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
	"github.com/andrewhigh08/team-task-nexus/internal/service"
)

// worker is a background job that runs until its context is canceled.
type worker interface {
	Run(ctx context.Context) error
}

// newDispatcher builds the outbox dispatcher, delivering through the sender the configuration
// selects.
func (a *App) newDispatcher() (*service.Dispatcher, error) {
	return service.NewDispatcher(mysqlrepo.NewOutboxRepo(a.db), a.newSender(), a.dispatcherConfig())
}

// dispatcherConfig bounds every delivery by the webhook timeout, which the log sender, writing
// to the log at once, never comes close to.
func (a *App) dispatcherConfig() service.DispatcherConfig {
	return service.DispatcherConfig{
		PollInterval: a.cfg.Outbox.PollInterval,
		BatchSize:    a.cfg.Outbox.BatchSize,
		MaxAttempts:  a.cfg.Outbox.MaxAttempts,
		Lease:        a.cfg.Outbox.Lease,
		SendTimeout:  a.cfg.Notify.WebhookTimeout,
	}
}

// newSender returns the webhook sender when a webhook is configured and the log sender
// otherwise.
func (a *App) newSender() port.EventSender {
	if a.cfg.Notify.WebhookURL == "" {
		return notify.NewLogSender(a.base.WithComponent("notify"))
	}
	return notify.NewWebhookSender(a.cfg.Notify.WebhookURL, a.cfg.Notify.WebhookTimeout)
}

// RunWorkers runs the background workers, which deliver the events of the outbox, until ctx is
// canceled. Run calls it; tests that serve Handler themselves call it directly.
func (a *App) RunWorkers(ctx context.Context) error {
	return a.dispatcher.Run(logger.NewContext(ctx, a.base.WithComponent("outbox")))
}

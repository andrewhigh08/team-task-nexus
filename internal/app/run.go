package app

import (
	"context"
	"errors"
	"fmt"
)

// Run serves HTTP until ctx is canceled or the server fails, then shuts the server down
// gracefully within SERVER_SHUTDOWN_TIMEOUT and releases every resource with Close.
func (a *App) Run(ctx context.Context) error {
	serveErr := make(chan error, 1)
	go func() {
		a.log.Info("http server listening", "addr", a.srv.Addr, "version", a.version)
		serveErr <- a.srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		// Shutdown has not been called yet, so the server did not stop on purpose.
		return errors.Join(fmt.Errorf("http server: %w", err), a.Close())
	case <-ctx.Done():
	}

	timeout := a.cfg.Server.ShutdownTimeout
	a.log.Info("shutting down", "timeout", timeout.String())
	// ctx is already canceled; in-flight requests get a deadline of their own to finish.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	var errs []error
	if err := a.srv.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("http server shutdown: %w", err))
	}
	errs = append(errs, a.Close())
	a.log.Info("shutdown complete")
	return errors.Join(errs...)
}

package app

import (
	"context"
	"errors"
	"fmt"
)

// Run serves HTTP and runs the background workers until ctx is canceled or the server fails.
// It then shuts the server down gracefully within SERVER_SHUTDOWN_TIMEOUT, stops the workers
// and waits for the delivery they have in flight, and releases every resource with Close.
func (a *App) Run(ctx context.Context) error {
	// The workers outlive ctx until the server has drained, so the events of the last
	// requests may still go out before the process exits.
	workersCtx, stopWorkers := context.WithCancel(context.WithoutCancel(ctx))
	defer stopWorkers()
	workersDone := make(chan error, 1)
	go func() { workersDone <- a.RunWorkers(workersCtx) }()

	serveErr := make(chan error, 1)
	go func() {
		a.log.Info("http server listening", "addr", a.srv.Addr, "version", a.version)
		serveErr <- a.srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		// Shutdown has not been called yet, so the server did not stop on purpose.
		stopWorkers()
		return errors.Join(fmt.Errorf("http server: %w", err), workersErr(<-workersDone), a.Close())
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
	stopWorkers()
	errs = append(errs, workersErr(<-workersDone), a.Close())
	a.log.Info("shutdown complete")
	return errors.Join(errs...)
}

func workersErr(err error) error {
	if err != nil {
		return fmt.Errorf("workers: %w", err)
	}
	return nil
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/emaforlin/url-shortener/internal/config"
)

// runServer serves handler until ctx is canceled, then shuts down gracefully.
//
// Cancellation of ctx (wired to SIGINT/SIGTERM in main) begins a graceful
// shutdown: the listener closes, in-flight requests are given
// cfg.ShutdownTimeout to finish, and only then are connections forced closed.
// Without this, a deploy would sever every request in progress.
func runServer(ctx context.Context, cfg config.Config, logger *slog.Logger, handler http.Handler) error {
	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,

		// ReadHeaderTimeout is the one that matters for slowloris: without it a
		// client can hold a connection open indefinitely by dribbling headers.
		ReadHeaderTimeout: cfg.ReadTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,

		// Route the server's own errors into the structured logger instead of
		// the standard logger's unstructured stderr output.
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	// Buffered so the goroutine can always exit, even if nobody reads the error
	// because shutdown won the race.
	serveErr := make(chan error, 1)

	go func() {
		logger.Info("server listening", "addr", srv.Addr, "env", cfg.Env)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		// ErrServerClosed means something else already called Shutdown; any
		// other error means the server never started or died unexpectedly.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen on %s: %w", srv.Addr, err)
		}
		return nil

	case <-ctx.Done():
		logger.Info("shutdown signal received", "grace_period", cfg.ShutdownTimeout)
		return shutdown(srv, logger, cfg.ShutdownTimeout)
	}
}

// shutdown drains the server, falling back to a hard close if draining overruns.
func shutdown(srv *http.Server, logger *slog.Logger, grace time.Duration) error {
	// A fresh context: the one that triggered this is already canceled, and
	// passing it to Shutdown would abandon in-flight requests immediately.
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		// Requests outlived the grace period. Close is the blunt instrument that
		// guarantees the process can actually exit.
		logger.Error("graceful shutdown timed out, forcing close", "error", err)
		if closeErr := srv.Close(); closeErr != nil {
			return fmt.Errorf("force close: %w", closeErr)
		}
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("shutdown complete")
	return nil
}

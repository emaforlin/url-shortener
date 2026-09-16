package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/emaforlin/url-shortener/internal/bootstrap"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// main does nothing but translate an error into an exit code. All real work
	// lives in run, so every deferred cleanup there actually executes — an
	// os.Exit inside run would skip them.
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Cancels ctx on the first SIGINT/SIGTERM, which is what an orchestrator
	// sends before it stops a container.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.New(version)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	return runServer(ctx, app)
}

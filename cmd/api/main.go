package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/emaforlin/url-shortener/internal/api"
	"github.com/emaforlin/url-shortener/internal/config"
	"github.com/emaforlin/url-shortener/internal/links"
	"github.com/emaforlin/url-shortener/internal/logging"
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

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := logging.New(os.Stdout, cfg)
	logger.Info("starting url-shortener",
		"version", version,
		"env", cfg.Env,
		"base_url", cfg.BaseURL,
		"log_level", cfg.LogLevel.String(),
	)

	// Dependencies are constructed here and injected downward, so swapping the
	// in-memory store for a database touches this wiring and nothing else.
	store := links.NewMemoryStore()
	service := links.NewService(store, cfg.BaseURL, logger)
	handler := api.NewHandler(service, version)

	router := api.NewRouter(handler, api.RouterConfig{
		RequestTimeout: cfg.RequestTimeout,
		MaxBodyBytes:   cfg.MaxBodyBytes,
	}, logger)

	return runServer(ctx, cfg, logger, router)
}

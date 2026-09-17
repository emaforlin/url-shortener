package bootstrap

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/emaforlin/url-shortener/internal/api"
	"github.com/emaforlin/url-shortener/internal/config"
	"github.com/emaforlin/url-shortener/internal/links"
	"github.com/emaforlin/url-shortener/internal/logging"
)

// App is the fully wired service, ready to be served by any entry point.
type App struct {
	Config  config.Config
	Logger  *slog.Logger
	Handler http.Handler
}

// New constructs a new App with all the dependencies.
// Is the only place where the where the object graph is assenbled
func New(version string) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	logger := logging.New(os.Stdout, cfg)
	logger.Info("starting url-shortener",
		"version", version,
		"env", cfg.Env,
		"base_url", cfg.BaseURL,
		"log_level", cfg.LogLevel.String(),
	)

	store := links.NewMemoryStore()
	service := links.NewService(store, cfg.BaseURL, logger)
	handler := api.NewHandler(service, version)

	router := api.NewRouter(handler, api.RouterConfig{
		RequestTimeout: cfg.RequestTimeout,
		MaxBodyBytes:   cfg.MaxBodyBytes,
	}, logger)

	return &App{
		Config:  cfg,
		Logger:  logger,
		Handler: router,
	}, nil
}

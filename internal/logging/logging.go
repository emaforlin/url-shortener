package logging

import (
	"context"
	"io"
	"log/slog"

	"github.com/emaforlin/url-shortener/internal/config"
)

// New builds the root logger: JSON in production so log aggregators can parse
// it, human-readable text in development.
func New(w io.Writer, cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}

	var handler slog.Handler
	if cfg.IsProduction() {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	logger := slog.New(handler)
	// Anything logging through the slog or log package defaults — including
	// dependencies — lands in the same stream with the same format.
	slog.SetDefault(logger)
	return logger
}

// ctxKey is unexported so no other package can collide with this context key.
type ctxKey struct{}

// WithLogger returns a context carrying logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// FromContext returns the logger stored by [WithLogger], falling back to the
// default logger so callers never need a nil check.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}

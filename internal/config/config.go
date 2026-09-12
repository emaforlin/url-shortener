package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Environment names recognised by [Config.Validate].
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// EnvFile is the dotenv file [Load] reads outside production.
const EnvFile = ".env"

// Config holds every tunable the service reads at startup.
type Config struct {
	Env             string        // Env selects human-readable or machine-readable logging.
	Port            string        // Port is the TCP port the HTTP server listens on.
	BaseURL         string        // BaseURL is the publicly reachable origin used to build short links.
	LogLevel        slog.Level    // LogLevel is the minimum level emitted by the logger.
	ReadTimeout     time.Duration // ReadTimeout bounds reading the whole request, headers and body.
	WriteTimeout    time.Duration // WriteTimeout bounds writing the response.
	IdleTimeout     time.Duration // IdleTimeout bounds how long a keep-alive connection may sit unused.
	RequestTimeout  time.Duration // RequestTimeout bounds a single handler invocation. Kept below WriteTimeout so the timeout response itself can still be written.
	ShutdownTimeout time.Duration // ShutdownTimeout bounds graceful shutdown before connections are forced closed.
	MaxBodyBytes    int64         // MaxBodyBytes caps the accepted request body size.
}

// Addr returns the listen address for [net/http.Server].
func (c Config) Addr() string { return ":" + c.Port }

// IsProduction reports whether the service runs in its production configuration.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// Load reads configuration from the environment, applies defaults for anything
// unset, and validates the result. The returned Config is safe to use only when
// the error is nil.
//
// Outside production it first folds [EnvFile] into the environment, so a
// developer only needs a checked-out repository to start the service.
func Load() (Config, error) {
	if err := loadDotenv(EnvFile); err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:      env("APP_ENV", EnvDevelopment),
		Port:     env("APP_PORT", "8080"),
		BaseURL:  env("PUBLIC_BASE_URL", ""),
		LogLevel: slog.LevelInfo,

		ReadTimeout:     5 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     120 * time.Second,
		RequestTimeout:  8 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		MaxBodyBytes:    64 << 10,
	}

	var errs []error

	if raw, ok := os.LookupEnv("LOG_LEVEL"); ok {
		level, err := parseLevel(raw)
		if err != nil {
			errs = append(errs, err)
		} else {
			cfg.LogLevel = level
		}
	}

	durations := map[string]*time.Duration{
		"APP_READ_TIMEOUT":     &cfg.ReadTimeout,
		"APP_WRITE_TIMEOUT":    &cfg.WriteTimeout,
		"APP_IDLE_TIMEOUT":     &cfg.IdleTimeout,
		"APP_REQUEST_TIMEOUT":  &cfg.RequestTimeout,
		"APP_SHUTDOWN_TIMEOUT": &cfg.ShutdownTimeout,
	}
	for key, target := range durations {
		if err := duration(key, target); err != nil {
			errs = append(errs, err)
		}
	}

	if raw, ok := os.LookupEnv("APP_MAX_BODY_BYTES"); ok {
		n, err := strconv.ParseInt(raw, 10, 64)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("APP_MAX_BODY_BYTES: %q is not an integer", raw))
		case n <= 0:
			errs = append(errs, fmt.Errorf("APP_MAX_BODY_BYTES: must be positive, got %d", n))
		default:
			cfg.MaxBodyBytes = n
		}
	}

	if err := cfg.Validate(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// Validate reports every problem with the configuration as a single joined error.
func (c Config) Validate() error {
	var errs []error

	if c.Env != EnvDevelopment && c.Env != EnvProduction {
		errs = append(errs, fmt.Errorf("APP_ENV: must be %q or %q, got %q", EnvDevelopment, EnvProduction, c.Env))
	}

	port, err := strconv.Atoi(c.Port)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("APP_PORT: %q is not a number", c.Port))
	case port < 1 || port > 65535:
		errs = append(errs, fmt.Errorf("APP_PORT: must be within 1-65535, got %d", port))
	}

	// BaseURL has no safe default: short links built against a guessed origin
	// would be silently wrong everywhere they are shared.
	switch u, err := url.Parse(c.BaseURL); {
	case c.BaseURL == "":
		errs = append(errs, errors.New("PUBLIC_BASE_URL: required, e.g. https://short.example.com"))
	case err != nil:
		errs = append(errs, fmt.Errorf("PUBLIC_BASE_URL: %q is not a valid URL: %v", c.BaseURL, err))
	case u.Scheme != "http" && u.Scheme != "https":
		errs = append(errs, fmt.Errorf("PUBLIC_BASE_URL: must be an absolute http(s) URL, got %q", c.BaseURL))
	case u.Host == "":
		errs = append(errs, fmt.Errorf("PUBLIC_BASE_URL: missing host in %q", c.BaseURL))
	}

	positive := map[string]time.Duration{
		"APP_READ_TIMEOUT":     c.ReadTimeout,
		"APP_WRITE_TIMEOUT":    c.WriteTimeout,
		"APP_IDLE_TIMEOUT":     c.IdleTimeout,
		"APP_REQUEST_TIMEOUT":  c.RequestTimeout,
		"APP_SHUTDOWN_TIMEOUT": c.ShutdownTimeout,
	}
	for key, d := range positive {
		if d <= 0 {
			errs = append(errs, fmt.Errorf("%s: must be positive, got %s", key, d))
		}
	}

	// A handler allowed to run longer than the write timeout can never deliver
	// its own timeout response, which shows up as a confusing truncated reply.
	if c.RequestTimeout > 0 && c.WriteTimeout > 0 && c.RequestTimeout >= c.WriteTimeout {
		errs = append(errs, fmt.Errorf(
			"APP_REQUEST_TIMEOUT (%s) must be shorter than APP_WRITE_TIMEOUT (%s)",
			c.RequestTimeout, c.WriteTimeout))
	}

	if c.MaxBodyBytes <= 0 {
		errs = append(errs, fmt.Errorf("APP_MAX_BODY_BYTES: must be positive, got %d", c.MaxBodyBytes))
	}

	return errors.Join(errs...)
}

// loadDotenv copies the assignments in path into the process environment.
//
// It is a development convenience and nothing more. In production every value
// has to come from the orchestrator, so a dotenv file found there is reported
// as a deployment mistake instead of being applied: a file baked into an image
// silently outliving the deploy that produced it is exactly the kind of stale
// configuration nobody notices until it is serving wrong short links.
//
// The production check reads APP_ENV from the process environment before the
// file is parsed, on purpose. Deciding after the file had been folded in would
// let the file exempt itself from the rule it is subject to.
func loadDotenv(path string) error {
	inProduction := os.Getenv("APP_ENV") == EnvProduction

	if _, err := os.Stat(path); err != nil {
		// No file is the normal case everywhere but a developer machine.
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%s: %w", path, err)
	}

	if inProduction {
		return fmt.Errorf(
			"%s: dotenv files are not loaded when APP_ENV=%s, set the configuration in the process environment",
			path, EnvProduction)
	}

	// Load, unlike Overload, leaves variables that are already set untouched, so
	// anything exported in the shell or injected by the runtime still wins.
	if err := godotenv.Load(path); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	// The file must not be able to opt the process into production either: that
	// would produce precisely what the check above refuses, a production
	// configuration sourced from a file.
	if os.Getenv("APP_ENV") == EnvProduction {
		return fmt.Errorf(
			"%s: must not set APP_ENV=%s, the deployment environment decides that",
			path, EnvProduction)
	}

	return nil
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func duration(key string, target *time.Duration) error {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("%s: %q is not a duration (try 5s, 200ms, 1m)", key, raw)
	}
	*target = d
	return nil
}

func parseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("LOG_LEVEL: must be debug, info, warn or error, got %q", raw)
	}
}

package api

import (
	"log/slog"
	"net/http"
	"time"
)

// RouterConfig holds the transport-level tunables the middleware needs.
//
// It deliberately mirrors a subset of config.Config rather than importing it, so
// this package stays independent of how configuration is sourced.
type RouterConfig struct {
	// RequestTimeout bounds a single handler invocation.
	RequestTimeout time.Duration
	// MaxBodyBytes caps the accepted request body size.
	MaxBodyBytes int64
}

// NewRouter builds the fully wrapped HTTP handler for the service.
func NewRouter(h *Handler, cfg RouterConfig, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// Operational endpoints. Liveness and readiness are separate on purpose:
	// see the doc comments on the handlers.
	mux.HandleFunc("GET /healthz", h.Health)
	mux.HandleFunc("GET /readyz", h.Ready)

	// The API is versioned from the first commit — adding a version prefix later
	// is a breaking change for every client already in the wild.
	mux.HandleFunc("POST /api/v1/links", h.CreateLink)

	// The bare-code redirect. Go 1.22+ pattern precedence means literal paths
	// such as /healthz win over this wildcard, so the probes stay reachable.
	mux.HandleFunc("GET /{code}", h.Redirect)

	// Catch-all for anything unmatched, so a stray request gets the same JSON
	// error envelope as every other failure instead of the stdlib's plain text.
	mux.HandleFunc("/", h.NotFound)

	// Order matters, and this list reads in the order a request flows through.
	//
	// requestID is outermost so every log line and error body below it carries
	// the correlation ID. accessLog sits above recoverer rather than below it, so
	// a panicking request is still logged — once recoverer has turned the panic
	// into a 500, accessLog records that status instead of losing the request
	// entirely as the stack unwinds. http.TimeoutHandler forwards handler panics
	// to its caller, so recoverer still covers everything beneath it.
	return chain(mux,
		requestID(logger),
		accessLog,
		recoverer,
		timeout(cfg.RequestTimeout),
		maxBodyBytes(cfg.MaxBodyBytes),
	)
}

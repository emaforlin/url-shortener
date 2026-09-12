package links

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

// Sentinel errors the transport layer maps to HTTP status codes. Callers must
// compare with errors.Is rather than by value, so wrapped errors keep working.
var (
	// ErrNotFound means no link exists for the requested code.
	ErrNotFound = errors.New("link not found")
	// ErrAlreadyExists means the code is already taken.
	ErrAlreadyExists = errors.New("link already exists")
	// ErrInvalidURL means the supplied target URL is not usable.
	ErrInvalidURL = errors.New("invalid target URL")
	// ErrExpired means the link existed but is past its expiry.
	ErrExpired = errors.New("link expired")
	// ErrNotImplemented marks the seams left for the real implementation.
	ErrNotImplemented = errors.New("not implemented")
)

// Link is a single shortened URL.
type Link struct {
	Code      string     `json:"code"`
	TargetURL string     `json:"target_url"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // nil means the link never expires.
	Hits      int64      `json:"hits"`
}

// IsExpired reports whether the link is past its expiry at time now.
func (l Link) IsExpired(now time.Time) bool {
	return l.ExpiresAt != nil && now.After(*l.ExpiresAt)
}

// Store is the persistence contract for links.
//
// Every method takes a context as its first parameter so a network-backed
// implementation can honour cancellation and deadlines without any signature
// change here or in the callers.
type Store interface {
	// Create stores a new link, returning ErrAlreadyExists if the code is taken.
	Create(ctx context.Context, l Link) error
	// GetByCode returns the link for code, or ErrNotFound.
	GetByCode(ctx context.Context, code string) (Link, error)
	// Delete removes the link for code, returning ErrNotFound if absent.
	Delete(ctx context.Context, code string) error
	// IncrementHits records a redirect for code. Best-effort: callers log
	// failures rather than failing the redirect.
	IncrementHits(ctx context.Context, code string) error
	// Ping reports whether the store is reachable, backing the readiness probe.
	Ping(ctx context.Context) error
}

// Service carries the shortening rules and is the only type handlers talk to.
type Service struct {
	store   Store
	baseURL string
	logger  *slog.Logger
}

// NewService wires a Service to its dependencies. A trailing slash on baseURL is
// trimmed so [Service.ShortURL] never emits a doubled separator.
func NewService(store Store, baseURL string, logger *slog.Logger) *Service {
	return &Service{
		store:   store,
		baseURL: strings.TrimRight(baseURL, "/"),
		logger:  logger,
	}
}

// Store exposes the underlying store for health checks.
func (s *Service) Store() Store { return s.store }

// ShortenRequest is the validated input to [Service.Shorten].
type ShortenRequest struct {
	// TargetURL is the destination to shorten.
	TargetURL string
	// CustomCode optionally requests a specific code instead of a generated one.
	CustomCode string
	// TTL optionally expires the link after the given duration. Zero means never.
	TTL time.Duration
}

// Shorten validates the request, allocates a code, and persists the link.
//
// TODO: implement. The steps this needs are:
//  1. Parse and validate TargetURL — require an http(s) scheme and a host, and
//     reject URLs pointing at this service's own BaseURL to avoid redirect loops.
//  2. Use CustomCode when supplied (after validating its charset and length),
//     otherwise generate one — base62 over crypto/rand is the usual choice.
//  3. Call store.Create, retrying on ErrAlreadyExists for generated codes since
//     a collision there is expected rather than a client error.
func (s *Service) Shorten(ctx context.Context, req ShortenRequest) (Link, error) {
	_ = ctx
	_ = req
	return Link{}, ErrNotImplemented
}

// Resolve returns the target URL for code and records the hit.
//
// TODO: implement. The steps this needs are:
//  1. Look the code up via store.GetByCode.
//  2. Treat an expired link as ErrExpired rather than returning its target.
//  3. Record the hit without failing the redirect if that bookkeeping errors.
func (s *Service) Resolve(ctx context.Context, code string) (Link, error) {
	_ = ctx
	_ = code
	return Link{}, ErrNotImplemented
}

// ShortURL builds the public short link for a code.
func (s *Service) ShortURL(code string) string {
	return s.baseURL + "/" + code
}

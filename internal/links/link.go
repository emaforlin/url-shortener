package links

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
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
	// ErrInvalidCode means a client-supplied custom code breaks the code rules.
	ErrInvalidCode = errors.New("invalid short code")
	// ErrExpired means the link existed but is past its expiry.
	ErrExpired = errors.New("link expired")
	// ErrNotImplemented marks the seams left for the real implementation.
	ErrNotImplemented = errors.New("not implemented")
)

// MinCodeLength and MaxCodeLength bound a client-supplied custom code. They are
// exported so the transport layer can state the rule in its error message
// without restating the numbers and drifting from them.
const (
	MinCodeLength = 4
	MaxCodeLength = 32
)

const (
	// codeAlphabet is base62
	codeAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	// unbiasedLimit is the largest multiple of the alphabet size that fits in a
	// byte. See [generateCode].
	unbiasedLimit = 256 - (256 % len(codeAlphabet))

	// generatedCodeLength is the length of an automatically allocated code.
	// Seven base62 characters is ~3.5e12 possibilities, which keeps a collision
	// rare enough that the retry below is a safety net rather than a normal path.
	generatedCodeLength = 7

	// maxCreateAttempts bounds that retry, so a store that wrongly reports every
	// write as a conflict fails the request instead of spinning.
	maxCreateAttempts = 5

	// maxTargetURLLength caps the destination. The body-size middleware already
	// bounds the request, but a shortener storing a 60 KB target is holding
	// something other than a link.
	maxTargetURLLength = 2048
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

func isValidURL(raw string) bool {
	if raw == "" || len(raw) > maxTargetURLLength {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	// Anything but http(s) in a Location header turns this into a redirect
	// gadget for javascript:, data: and file: payloads.
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// isSelfReferential reports whether raw points back at this service, which would
// resolve a short link to another short link.
func (s *Service) isSelfReferential(raw string) bool {
	target, err := url.Parse(raw)
	if err != nil {
		return false
	}
	base, err := url.Parse(s.baseURL)
	if err != nil {
		return false
	}
	return base.Host != "" && strings.EqualFold(target.Host, base.Host)
}

func isValidCode(c string) bool {
	if len(c) < MinCodeLength || len(c) > MaxCodeLength {
		return false
	}
	for _, r := range c {
		if !strings.ContainsRune(codeAlphabet, r) {
			return false
		}
	}
	return true
}

// generateCode returns n random base62 characters.
//
// Bytes at or above unbiasedLimit are discarded rather than folded with %: 256
// is not a multiple of 62, so reducing the whole byte range would make the first
// eight characters of the alphabet more likely than the rest.
func generateCode(n int) (string, error) {
	code := make([]byte, 0, n)
	buf := make([]byte, n)

	for len(code) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("read random bytes: %w", err)
		}
		for _, b := range buf {
			if int(b) >= unbiasedLimit {
				continue
			}
			code = append(code, codeAlphabet[int(b)%len(codeAlphabet)])
			if len(code) == n {
				break
			}
		}
	}
	return string(code), nil
}

// Shorten validates the request, allocates a code, and persists the link.
//
// It returns ErrInvalidURL for an unusable target, ErrInvalidCode for a
// malformed CustomCode and ErrAlreadyExists for one that is taken.
func (s *Service) Shorten(ctx context.Context, req ShortenRequest) (Link, error) {
	if !isValidURL(req.TargetURL) || s.isSelfReferential(req.TargetURL) {
		return Link{}, ErrInvalidURL
	}

	link := Link{
		TargetURL: req.TargetURL,
		CreatedAt: time.Now().UTC(),
	}
	if req.TTL > 0 {
		expiresAt := link.CreatedAt.Add(req.TTL)
		link.ExpiresAt = &expiresAt
	}

	if req.CustomCode != "" {
		if !isValidCode(req.CustomCode) {
			return Link{}, ErrInvalidCode
		}
		link.Code = req.CustomCode

		// Only the client can pick a different code, so a conflict on their own
		// choice is reported rather than worked around.
		if err := s.store.Create(ctx, link); err != nil {
			return Link{}, err
		}
		return link, nil
	}

	for attempt := 1; attempt <= maxCreateAttempts; attempt++ {
		code, err := generateCode(generatedCodeLength)
		if err != nil {
			return Link{}, err
		}
		link.Code = code

		switch err := s.store.Create(ctx, link); {
		case err == nil:
			return link, nil
		case errors.Is(err, ErrAlreadyExists):
			s.logger.WarnContext(ctx, "generated code collided", "code", code, "attempt", attempt)
		default:
			return Link{}, err
		}
	}

	// Deliberately not ErrAlreadyExists: the client supplied no code, so a 409
	// would blame them for a keyspace this service is responsible for.
	return Link{}, fmt.Errorf("no unused code after %d attempts", maxCreateAttempts)
}

// Resolve returns the target URL for code and records the hit.
func (s *Service) Resolve(ctx context.Context, code string) (Link, error) {
	link, err := s.store.GetByCode(ctx, code)
	switch {
	case err == nil:
		if link.IsExpired(time.Now().UTC()) {
			return Link{}, ErrExpired
		}
		// Record the hit, but don't fail the redirect if that errors.
		if err := s.store.IncrementHits(ctx, code); err != nil {
			s.logger.WarnContext(ctx, "failed to increment hits", "code", code, "error", err)
		}
		return link, nil
	case errors.Is(err, ErrNotFound):
		return Link{}, ErrNotFound
	default:
		return Link{}, err
	}
}

// ShortURL builds the public short link for a code.
func (s *Service) ShortURL(code string) string {
	return s.baseURL + "/" + code
}

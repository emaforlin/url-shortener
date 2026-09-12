package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/emaforlin/url-shortener/internal/links"
	"github.com/emaforlin/url-shortener/internal/logging"
)

// Handler holds the dependencies every HTTP handler needs. Dependencies are
// injected rather than reached for as globals, so tests can supply fakes.
type Handler struct {
	links   *links.Service
	version string
}

// NewHandler wires a Handler to its dependencies.
func NewHandler(svc *links.Service, version string) *Handler {
	return &Handler{links: svc, version: version}
}

// Health is the liveness probe: it answers as long as the process can serve.
// It must not check dependencies, or a brief database blip would make an
// orchestrator kill an otherwise healthy process.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": h.version,
	})
}

// Ready is the readiness probe: it reports whether this instance can serve
// traffic right now, so a failing dependency removes it from the load balancer
// without restarting it.
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.links.Store().Ping(ctx); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "readiness check failed", "error", err)
		respondJSON(w, r, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"reason": "store unreachable",
		})
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
}

// createLinkRequest is the JSON body accepted by [Handler.CreateLink].
type createLinkRequest struct {
	URL string `json:"url"`
	// Code optionally requests a specific short code.
	Code string `json:"code,omitempty"`
	// TTLSeconds optionally expires the link. Zero or absent means never.
	TTLSeconds int64 `json:"ttl_seconds,omitempty"`
}

// createLinkResponse is returned on a successful shorten.
type createLinkResponse struct {
	Code      string     `json:"code"`
	ShortURL  string     `json:"short_url"`
	TargetURL string     `json:"target_url"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateLink shortens a URL.
//
// The transport work — decoding, validation of the request shape, status mapping
// — is complete. The domain call it delegates to is the stub, so this currently
// answers 501; implementing [links.Service.Shorten] makes the endpoint live.
func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	var req createLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, r, err.status, err.code, err.message)
		return
	}

	if req.TTLSeconds < 0 {
		respondError(w, r, http.StatusBadRequest, CodeBadRequest, "ttl_seconds must not be negative")
		return
	}

	link, err := h.links.Shorten(r.Context(), links.ShortenRequest{
		TargetURL:  req.URL,
		CustomCode: req.Code,
		TTL:        time.Duration(req.TTLSeconds) * time.Second,
	})
	if err != nil {
		respondDomainError(w, r, err)
		return
	}

	w.Header().Set("Location", h.links.ShortURL(link.Code))
	respondJSON(w, r, http.StatusCreated, createLinkResponse{
		Code:      link.Code,
		ShortURL:  h.links.ShortURL(link.Code),
		TargetURL: link.TargetURL,
		ExpiresAt: link.ExpiresAt,
	})
}

// Redirect sends a client from a short code to its target.
//
// As with CreateLink, the transport side is done and the domain call is the stub.
func (h *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		respondError(w, r, http.StatusNotFound, CodeNotFound, "short link not found")
		return
	}

	link, err := h.links.Resolve(r.Context(), code)
	if err != nil {
		respondDomainError(w, r, err)
		return
	}

	// 302 rather than 301: a permanent redirect is cached by browsers
	// indefinitely, which makes a link impossible to retarget or revoke.
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, link.TargetURL, http.StatusFound)
}

// NotFound answers unmatched routes in the standard JSON envelope, replacing the
// stdlib's plain-text 404 so clients only ever parse one error shape.
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	respondError(w, r, http.StatusNotFound, CodeNotFound, "resource not found")
}

// decodeError carries the HTTP translation of a request-decoding failure.
type decodeError struct {
	status  int
	code    string
	message string
}

func (e *decodeError) Error() string { return e.message }

// decodeJSON strictly decodes a JSON request body into dst.
//
// Unknown fields are rejected so a typo in a client payload surfaces as a 400
// rather than being silently ignored, and trailing content is rejected so two
// concatenated objects cannot be mistaken for one.
// The body size cap is applied by the maxBodyBytes middleware, so this only has
// to translate the resulting error.
func decodeJSON(r *http.Request, dst any) *decodeError {
	if ct := r.Header.Get("Content-Type"); ct != "" && !isJSONContentType(ct) {
		return &decodeError{http.StatusUnsupportedMediaType, CodeBadRequest,
			"Content-Type must be application/json"}
	}

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxBytes *http.MaxBytesError
		switch {
		case errors.As(err, &maxBytes):
			return &decodeError{http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
				"request body is too large"}
		case errors.Is(err, io.EOF):
			return &decodeError{http.StatusBadRequest, CodeBadRequest,
				"request body must not be empty"}
		default:
			// json error text echoes offsets and field names from the client's
			// own payload, so it is safe to return and genuinely useful.
			return &decodeError{http.StatusBadRequest, CodeBadRequest,
				"malformed JSON: " + err.Error()}
		}
	}

	if dec.More() {
		return &decodeError{http.StatusBadRequest, CodeBadRequest,
			"request body must contain a single JSON object"}
	}
	return nil
}

func isJSONContentType(ct string) bool {
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}

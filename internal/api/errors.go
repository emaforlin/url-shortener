package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/emaforlin/url-shortener/internal/links"
	"github.com/emaforlin/url-shortener/internal/logging"
)

// respondDomainError maps a domain error to its HTTP status and writes the
// standard envelope. Handlers call this instead of choosing status codes inline,
// so one sentinel maps to one status everywhere.
//
// Unrecognised errors are deliberately reported as a generic 500: the full error
// goes to the log, never to the client, since internal messages leak table names,
// host names and query fragments.
func respondDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, links.ErrNotFound):
		respondError(w, r, http.StatusNotFound, CodeNotFound, "short link not found")

	case errors.Is(err, links.ErrAlreadyExists):
		respondError(w, r, http.StatusConflict, CodeConflict, "that code is already taken")

	case errors.Is(err, links.ErrInvalidURL):
		respondError(w, r, http.StatusBadRequest, CodeBadRequest, "target URL is not a valid http(s) URL")

	case errors.Is(err, links.ErrExpired):
		respondError(w, r, http.StatusGone, CodeGone, "short link has expired")

	case errors.Is(err, links.ErrNotImplemented):
		respondError(w, r, http.StatusNotImplemented, CodeNotImplemented, "endpoint is not implemented yet")

	case errors.Is(err, context.DeadlineExceeded):
		respondError(w, r, http.StatusGatewayTimeout, CodeTimeout, "request timed out")

	case errors.Is(err, context.Canceled):
		// The client went away; no response will be read, so just record it.
		logging.FromContext(r.Context()).DebugContext(r.Context(),
			"request canceled by client", "path", r.URL.Path)

	default:
		logging.FromContext(r.Context()).ErrorContext(r.Context(),
			"unhandled error", "error", err, "method", r.Method, "path", r.URL.Path)
		respondError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
	}
}

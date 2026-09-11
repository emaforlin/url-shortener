package api

import (
	"encoding/json"
	"net/http"

	"github.com/emaforlin/url-shortener/internal/logging"
)

// Machine-readable error codes. Clients branch on these rather than on the
// human-readable message, which is free to change.
const (
	CodeBadRequest     = "bad_request"
	CodeNotFound       = "not_found"
	CodeConflict       = "conflict"
	CodeGone           = "gone"
	CodePayloadTooLarge = "payload_too_large"
	CodeTimeout        = "timeout"
	CodeInternal       = "internal_error"
	CodeNotImplemented = "not_implemented"
)

// ErrorResponse is the single error envelope used by every failure path,
// including 404s and recovered panics, so clients only ever parse one shape.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody carries the details of a failed request.
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// respondJSON writes payload as JSON with the given status.
//
// The body is encoded into a buffer first: encoding straight to the
// ResponseWriter would commit a 200 header before a mid-encode failure could be
// reported, leaving the client with a truncated body and a misleading status.
func respondJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		logging.FromContext(r.Context()).ErrorContext(r.Context(),
			"encoding response failed", "error", err, "path", r.URL.Path)
		respondError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(body); err != nil {
		// The status line is already sent, so this can only be logged.
		logging.FromContext(r.Context()).WarnContext(r.Context(),
			"writing response failed", "error", err, "path", r.URL.Path)
	}
}

// respondError writes the standard error envelope, tagged with the request ID so
// a user-visible failure can be traced back to its log lines.
func respondError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	payload := ErrorResponse{Error: ErrorBody{
		Code:      code,
		Message:   message,
		RequestID: RequestIDFromContext(r.Context()),
	}}

	body, err := json.Marshal(payload)
	if err != nil {
		// Unreachable for this fixed struct, but never leave the client hanging.
		http.Error(w, `{"error":{"code":"internal_error","message":"internal server error"}}`,
			http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

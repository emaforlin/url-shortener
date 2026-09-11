package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/emaforlin/url-shortener/internal/logging"
)

// RequestIDHeader carries the correlation ID in and out of the service.
const RequestIDHeader = "X-Request-Id"

// maxForwardedRequestIDLen caps a client-supplied request ID. Echoing an
// unbounded header back would let a caller inflate every log line it touches.
const maxForwardedRequestIDLen = 64

// middleware wraps a handler with one cross-cutting concern.
type middleware func(http.Handler) http.Handler

// chain applies middlewares so the first argument ends up outermost, letting the
// call site read in the order requests actually flow through.
func chain(h http.Handler, middlewares ...middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

type requestIDKey struct{}

// RequestIDFromContext returns the request ID assigned by [requestID], or "".
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// requestID assigns every request a correlation ID, reusing a sane inbound one so
// a trace started at the load balancer survives into these logs. The ID is echoed
// on the response and attached to the context logger.
func requestID(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := sanitizeRequestID(r.Header.Get(RequestIDHeader))
			if id == "" {
				id = newRequestID()
			}

			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			ctx = logging.WithLogger(ctx, logger.With("request_id", id))

			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// sanitizeRequestID accepts an inbound ID only if it is short and printable ASCII.
// Unvalidated header content reaching logs enables forged or broken log lines.
func sanitizeRequestID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxForwardedRequestIDLen {
		return ""
	}
	for _, c := range raw {
		if c < 0x20 || c > 0x7e {
			return ""
		}
	}
	return raw
}

func newRequestID() string {
	var b [16]byte
	// rand.Read from crypto/rand never returns an error as of Go 1.24.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// accessLog records one line per request once the response is complete.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		logger := logging.FromContext(r.Context())
		level := slog.LevelInfo
		switch {
		case rec.status >= http.StatusInternalServerError:
			level = slog.LevelError
		case rec.status >= http.StatusBadRequest:
			level = slog.LevelWarn
		}

		logger.LogAttrs(r.Context(), level, "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int64("bytes", rec.written),
			slog.Duration("duration", time.Since(start)),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("user_agent", r.UserAgent()),
		)
	})
}

// statusRecorder captures the status and size of a response for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	written     int64
	wroteHeader bool
}

func (rec *statusRecorder) WriteHeader(status int) {
	if rec.wroteHeader {
		// The stdlib already warns about a duplicate WriteHeader; keep the
		// first status so the log matches what the client actually received.
		return
	}
	rec.status = status
	rec.wroteHeader = true
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	if !rec.wroteHeader {
		rec.WriteHeader(http.StatusOK)
	}
	n, err := rec.ResponseWriter.Write(b)
	rec.written += int64(n)
	return n, err
}

// Flush keeps streaming responses working through the wrapper.
func (rec *statusRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer, so deadline
// and hijack support are not silently lost by wrapping.
func (rec *statusRecorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

// recoverer converts a panic into a logged 500 instead of killing the process and
// dropping every in-flight connection. It belongs outermost so it also covers
// panics raised inside the other middleware.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// ErrAbortHandler is the stdlib's documented way to abandon a
			// response silently; honour that contract instead of logging it.
			if rec == http.ErrAbortHandler {
				panic(rec)
			}

			logging.FromContext(r.Context()).ErrorContext(r.Context(), "recovered from panic",
				"panic", rec,
				"method", r.Method,
				"path", r.URL.Path,
				"stack", string(debug.Stack()),
			)
			respondError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
		}()

		next.ServeHTTP(w, r)
	})
}

// timeout bounds how long a single handler may run. The duration must stay below
// the server's write timeout so this response can still reach the client.
func timeout(d time.Duration) middleware {
	body, _ := json.Marshal(ErrorResponse{Error: ErrorBody{
		Code:    CodeTimeout,
		Message: "request timed out",
	}})
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, string(body))
	}
}

// maxBodyBytes caps the request body so a large or endless upload cannot exhaust
// memory. Handlers see io.EOF-style failures from the reader once the cap is hit.
func maxBodyBytes(n int64) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

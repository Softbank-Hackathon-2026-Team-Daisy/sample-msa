// Package httpx holds the HTTP plumbing shared by every service: request IDs,
// structured request logging, panic recovery, security headers, JSON
// responses, health/readiness probes and graceful serving.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

// RequestIDHeader carries the request ID between clients and services.
const RequestIDHeader = "X-Request-ID"

// maxRequestIDLen bounds the size of propagated request IDs.
const maxRequestIDLen = 128

// SecurityHeaders are set on every response by Wrap.
var SecurityHeaders = map[string]string{
	"Content-Security-Policy":      "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
	"X-Content-Type-Options":       "nosniff",
	"X-Frame-Options":              "DENY",
	"Referrer-Policy":              "no-referrer",
	"Cross-Origin-Opener-Policy":   "same-origin",
	"Cross-Origin-Resource-Policy": "same-origin",
	"Permissions-Policy":           "camera=(), microphone=(), geolocation=()",
}

type ctxKey struct{}

// RequestID returns the request ID stored in ctx, if any.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Wrap applies the standard middleware chain to h.
func Wrap(logger *slog.Logger, h http.Handler) http.Handler {
	return withRequestID(withLogging(logger, withRecover(logger, withSecurityHeaders(h))))
}

// withRequestID reuses a well-formed inbound X-Request-ID or generates a new
// one, stores it in the request context and echoes it in the response.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

// validRequestID accepts 1-128 visible ASCII characters, which covers UUIDs
// and the formats used by common proxies and load balancers.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x21 || id[i] > 0x7e {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error.
	return hex.EncodeToString(b[:])
}

// statusRecorder captures the response status and size for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func isProbe(path string) bool {
	return path == "/health" || path == "/healthz" || path == "/readyz"
}

// withLogging emits one structured log record per request. Probe endpoints
// are logged at debug level to keep production logs readable.
func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}

		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case isProbe(r.URL.Path):
			level = slog.LevelDebug
		}

		logger.LogAttrs(r.Context(), level, "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.Int("bytes", rec.bytes),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("request_id", RequestID(r.Context())),
		)
	})
}

// withRecover converts handler panics into a generic 500 response without
// leaking details to the client.
func withRecover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.ErrorContext(r.Context(), "handler panic",
					slog.Any("panic", v),
					slog.String("request_id", RequestID(r.Context())),
				)
				WriteError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		for k, v := range SecurityHeaders {
			h.Set(k, v)
		}
		next.ServeHTTP(w, r)
	})
}

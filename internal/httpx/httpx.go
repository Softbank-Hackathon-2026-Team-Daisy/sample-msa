package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// HTTP server limits.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 16 << 10
)

// NewLogger returns a JSON logger writing to w. Every record carries the
// service name; the time field is named "timestamp" (RFC 3339, UTC).
func NewLogger(w io.Writer, level slog.Leveler, service string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Key = "timestamp"
				a.Value = slog.StringValue(a.Value.Time().UTC().Format(time.RFC3339Nano))
			}
			return a
		},
	})).With(slog.String("service", service))
}

// WriteJSON writes v as a JSON response that must not be cached.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"error":"internal server error"}`)
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

// WriteError writes {"error": msg}.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// Probes serves liveness (/health, /healthz) and readiness (/readyz).
// Liveness never depends on anything else. Readiness requires the server to
// be serving and, if a dependency check is configured, that check to pass.
type Probes struct {
	ready atomic.Bool
	check func(context.Context) error
}

// NewProbes returns probes. check may be nil; its error message is returned
// to callers, so it must not contain sensitive detail.
func NewProbes(check func(context.Context) error) *Probes {
	return &Probes{check: check}
}

// SetReady sets the local readiness flag.
func (p *Probes) SetReady(ready bool) { p.ready.Store(ready) }

// Register adds the probe routes to mux.
func (p *Probes) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", p.health)
	mux.HandleFunc("GET /healthz", p.health)
	mux.HandleFunc("GET /readyz", p.readyz)
}

func (p *Probes) health(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Probes) readyz(w http.ResponseWriter, r *http.Request) {
	if !p.ready.Load() {
		WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
		return
	}
	if p.check != nil {
		if err := p.check(r.Context()); err != nil {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready", "reason": err.Error()})
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// Serve serves h on ln until ctx is cancelled, then shuts down gracefully:
// readiness is withdrawn, the listener is closed, and in-flight requests get
// up to shutdownTimeout to complete.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, shutdownTimeout time.Duration, logger *slog.Logger, setReady func(bool)) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	setReady(true)
	logger.Info("server listening", slog.String("addr", ln.Addr().String()))

	select {
	case err := <-errCh:
		setReady(false)
		return err
	case <-ctx.Done():
	}

	setReady(false)
	logger.Info("shutting down", slog.String("timeout", shutdownTimeout.String()))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return err
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("server stopped")
	return nil
}

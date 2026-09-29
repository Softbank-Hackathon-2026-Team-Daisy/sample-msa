package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func get(h http.Handler, path string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestProbes(t *testing.T) {
	var failing error
	p := NewProbes(func(context.Context) error { return failing })
	mux := http.NewServeMux()
	p.Register(mux)

	if rec := get(mux, "/readyz", nil); rec.Code != 503 {
		t.Fatalf("readyz before SetReady = %d, want 503", rec.Code)
	}
	p.SetReady(true)
	if rec := get(mux, "/readyz", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ready"`) {
		t.Fatalf("readyz = %d %s", rec.Code, rec.Body)
	}
	failing = errors.New("dependency down")
	rec := get(mux, "/readyz", nil)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), `"reason":"dependency down"`) {
		t.Fatalf("readyz with failing check = %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/health", "/healthz"} {
		if rec := get(mux, path, nil); rec.Code != 200 {
			t.Fatalf("%s = %d; liveness must ignore dependency checks", path, rec.Code)
		}
	}
}

func TestWrap(t *testing.T) {
	var logs bytes.Buffer
	logger := NewLogger(&logs, slog.LevelDebug, "svc")
	h := Wrap(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]string{"id": RequestID(r.Context())})
	}))

	rec := get(h, "/thing?secret=1", map[string]string{"X-Request-ID": "abc-123"})
	if rec.Header().Get("X-Request-ID") != "abc-123" || !strings.Contains(rec.Body.String(), "abc-123") {
		t.Fatalf("request ID not propagated: %v %s", rec.Header(), rec.Body)
	}
	for k := range SecurityHeaders {
		if rec.Header().Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
	if id := get(h, "/", map[string]string{"X-Request-ID": strings.Repeat("a", 200)}).Header().Get("X-Request-ID"); len(id) != 32 {
		t.Fatalf("oversized ID should be replaced, got %q", id)
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.SplitN(logs.Bytes(), []byte("\n"), 2)[0], &entry); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"timestamp", "level", "msg", "service", "method", "path", "status", "duration_ms", "request_id"} {
		if _, ok := entry[k]; !ok {
			t.Errorf("log missing %q: %v", k, entry)
		}
	}
	if entry["service"] != "svc" || entry["path"] != "/thing" || strings.Contains(logs.String(), "secret") {
		t.Errorf("unexpected log: %s", logs.String())
	}
}

func TestRecoverHidesPanicDetails(t *testing.T) {
	var logs bytes.Buffer
	h := Wrap(NewLogger(&logs, slog.LevelInfo, "svc"), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("sensitive detail")
	}))
	rec := get(h, "/", nil)
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "sensitive") || !strings.Contains(logs.String(), "sensitive detail") {
		t.Fatalf("status %d body %s logs %s", rec.Code, rec.Body, logs.String())
	}
}

func TestServeGracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, "done")
	})
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan bool, 4)
	serveErr := make(chan error, 1)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	go func() { serveErr <- Serve(ctx, ln, h, 5*time.Second, logger, func(r bool) { ready <- r }) }()
	if !<-ready {
		t.Fatal("not ready")
	}

	url := "http://" + ln.Addr().String() + "/"
	body := make(chan string, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil {
			body <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()
	<-started
	cancel()
	if <-ready {
		t.Fatal("readiness not withdrawn on shutdown")
	}
	if got := <-body; got != "done" {
		t.Fatalf("in-flight request = %q", got)
	}
	if err := <-serveErr; err != nil {
		t.Fatalf("Serve = %v", err)
	}
	if _, err := http.Get(url); err == nil {
		t.Fatal("still accepting connections")
	}
}

func TestServeShutdownTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(started); <-release })
	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	go func() { serveErr <- Serve(ctx, ln, h, 100*time.Millisecond, logger, func(bool) {}) }()
	go func() {
		if resp, err := http.Get("http://" + ln.Addr().String() + "/"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	cancel()
	select {
	case err := <-serveErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Serve = %v, want deadline exceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown ignored its timeout")
	}
}

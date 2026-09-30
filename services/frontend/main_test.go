package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/buildinfo"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/httpx"
)

// fakeBackend imitates the backend service and records what it received.
type fakeBackend struct {
	mu       sync.Mutex
	ready    bool
	lastReq  *http.Request
	lastBody string
}

func (b *fakeBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	b.mu.Lock()
	b.lastReq, b.lastBody = r.Clone(r.Context()), string(body)
	ready := b.ready
	b.mu.Unlock()

	w.Header().Set(httpx.RequestIDHeader, r.Header.Get(httpx.RequestIDHeader))
	w.Header().Set("X-Frame-Options", "DENY")
	switch r.URL.Path {
	case "/readyz", "/prefix/readyz":
		if !ready {
			httpx.WriteJSON(w, 503, map[string]string{"status": "not ready"})
			return
		}
		httpx.WriteJSON(w, 200, map[string]string{"status": "ready"})
	case "/version", "/prefix/version":
		httpx.WriteJSON(w, 200, buildinfo.Get("hellocalc-backend"))
	case "/api/calculate", "/prefix/api/calculate":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		httpx.WriteJSON(w, 200, map[string]float64{"result": 50})
	default:
		http.NotFound(w, r)
	}
}

func (b *fakeBackend) last() (*http.Request, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastReq, b.lastBody
}

func newFrontend(t *testing.T, backendURL string) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	env := map[string]string{"BACKEND_URL": backendURL}
	mux, probes, err := build(func(k string) string { return env[k] }, logger, buildinfo.Get(serviceName))
	if err != nil {
		t.Fatal(err)
	}
	probes.SetReady(true)
	return httpx.Wrap(logger, mux)
}

func setup(t *testing.T) (*fakeBackend, http.Handler) {
	t.Helper()
	fb := &fakeBackend{ready: true}
	srv := httptest.NewServer(fb)
	t.Cleanup(srv.Close)
	return fb, newFrontend(t, srv.URL)
}

func do(t *testing.T, h http.Handler, method, path, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body, err)
	}
	return m
}

func TestProxyForwardsToBackend(t *testing.T) {
	fb, h := setup(t)
	body := `{"left":12.5,"operator":"*","right":4}`
	rec := do(t, h, http.MethodPost, "/api/calculate", body, map[string]string{
		"Content-Type": "application/json",
		"X-Request-ID": "trace-123",
	})
	if rec.Code != 200 || decode(t, rec)["result"] != float64(50) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}

	req, gotBody := fb.last()
	if req.URL.Path != "/api/calculate" || req.Method != http.MethodPost || gotBody != body {
		t.Fatalf("backend got %s %s %q", req.Method, req.URL.Path, gotBody)
	}
	if got := req.Header.Get("X-Request-ID"); got != "trace-123" {
		t.Fatalf("backend X-Request-ID = %q, want trace-123", got)
	}
	if req.Header.Get("X-Forwarded-For") == "" || req.Header.Get("X-Forwarded-Host") == "" {
		t.Fatalf("missing X-Forwarded headers: %v", req.Header)
	}
	if got := rec.Header().Values("X-Request-ID"); len(got) != 1 || got[0] != "trace-123" {
		t.Fatalf("response X-Request-ID = %v, want exactly [trace-123]", got)
	}
	if got := rec.Header().Values("X-Frame-Options"); len(got) != 1 {
		t.Fatalf("security header duplicated: %v", got)
	}
}

func TestProxyGeneratesRequestID(t *testing.T) {
	fb, h := setup(t)
	rec := do(t, h, http.MethodPost, "/api/calculate", `{}`, map[string]string{"Content-Type": "application/json"})
	req, _ := fb.last()
	id := rec.Header().Get("X-Request-ID")
	if len(id) != 32 || req.Header.Get("X-Request-ID") != id {
		t.Fatalf("generated ID %q not propagated (backend saw %q)", id, req.Header.Get("X-Request-ID"))
	}
}

func TestProxyPassesBackendStatus(t *testing.T) {
	_, h := setup(t)
	rec := do(t, h, http.MethodGet, "/api/calculate", "", nil)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
		t.Fatalf("status %d Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestBackendURLWithPathPrefix(t *testing.T) {
	fb := &fakeBackend{ready: true}
	srv := httptest.NewServer(fb)
	defer srv.Close()
	h := newFrontend(t, srv.URL+"/prefix/")
	rec := do(t, h, http.MethodPost, "/api/calculate", `{}`, map[string]string{"Content-Type": "application/json"})
	req, _ := fb.last()
	if rec.Code != 200 || req.URL.Path != "/prefix/api/calculate" {
		t.Fatalf("status %d, backend path %q", rec.Code, req.URL.Path)
	}
	if rec := do(t, h, http.MethodGet, "/readyz", "", nil); rec.Code != 200 {
		t.Fatalf("readyz with prefix = %d", rec.Code)
	}
}

func TestBackendDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens at url any more
	h := newFrontend(t, url)

	rec := do(t, h, http.MethodPost, "/api/calculate", `{}`, map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadGateway || decode(t, rec)["error"] != "backend unavailable" {
		t.Fatalf("api: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, h, http.MethodGet, "/readyz", "", nil)
	if m := decode(t, rec); rec.Code != 503 || m["reason"] != "backend unreachable" {
		t.Fatalf("readyz: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, http.MethodGet, "/health", "", nil); rec.Code != 200 {
		t.Fatalf("liveness must not depend on backend: %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/version", "", nil)
	if m := decode(t, rec); rec.Code != 200 || m["name"] != serviceName || m["backend"] != nil {
		t.Fatalf("version: %d %s", rec.Code, rec.Body)
	}
}

func TestReadinessFollowsBackend(t *testing.T) {
	fb, h := setup(t)
	if rec := do(t, h, http.MethodGet, "/readyz", "", nil); rec.Code != 200 || decode(t, rec)["status"] != "ready" {
		t.Fatalf("ready backend: %d %s", rec.Code, rec.Body)
	}
	fb.mu.Lock()
	fb.ready = false
	fb.mu.Unlock()
	rec := do(t, h, http.MethodGet, "/readyz", "", nil)
	if m := decode(t, rec); rec.Code != 503 || m["reason"] != "backend not ready" {
		t.Fatalf("unready backend: %d %s", rec.Code, rec.Body)
	}
}

func TestVersionIncludesBackend(t *testing.T) {
	_, h := setup(t)
	rec := do(t, h, http.MethodGet, "/version", "", nil)
	m := decode(t, rec)
	backend, _ := m["backend"].(map[string]any)
	if rec.Code != 200 || m["name"] != serviceName || backend["name"] != "hellocalc-backend" || m["commit"] == nil {
		t.Fatalf("version: %d %s", rec.Code, rec.Body)
	}
}

func TestStaticAssets(t *testing.T) {
	_, h := setup(t)
	for path, want := range map[string]string{"/": "<title>HelloCalc</title>", "/app.js": "/api/calculate", "/styles.css": ".keys"} {
		rec := do(t, h, http.MethodGet, path, "", nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET %s: %d", path, rec.Code)
		}
	}
	if rec := do(t, h, http.MethodGet, "/main.go", "", nil); rec.Code != 404 {
		t.Errorf("GET /main.go = %d, want 404", rec.Code)
	}
}

func TestParseBackendURL(t *testing.T) {
	for _, ok := range []string{"http://backend:8080", "https://api.example.com/base/", " http://10.0.0.5:9000 "} {
		if _, err := parseBackendURL(ok); err != nil {
			t.Errorf("%q: unexpected error %v", ok, err)
		}
	}
	for _, bad := range []string{"", "backend:8080", "ftp://backend", "http://", "http://backend:8080?x=1", "/api"} {
		if _, err := parseBackendURL(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

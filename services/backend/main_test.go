package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/buildinfo"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/httpx"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	mux, probes, err := build(func(string) string { return "" }, logger, buildinfo.Get(serviceName))
	if err != nil {
		t.Fatal(err)
	}
	probes.SetReady(true)
	return httpx.Wrap(logger, mux)
}

func do(t *testing.T, h http.Handler, method, path, contentType, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var m map[string]any
	if rec.Header().Get("Content-Type") == "application/json" {
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("invalid JSON %q: %v", rec.Body, err)
		}
	}
	return rec, m
}

func TestCalculate(t *testing.T) {
	h := newHandler(t)
	tests := []struct {
		body string
		want float64
	}{
		{`{"left":1,"operator":"+","right":2}`, 3},
		{`{"left":10,"operator":"/","right":4}`, 2.5},
		{`{"left":-7,"operator":"*","right":3}`, -21},
		{`{"left":12.5,"operator":"*","right":4}`, 50},
		{`{"left":7,"operator":"%","right":4}`, 3},
		{`{"left":2,"operator":"^","right":8}`, 256},
	}
	for _, tt := range tests {
		rec, m := do(t, h, http.MethodPost, "/api/calculate", "application/json", tt.body)
		if rec.Code != http.StatusOK || m["result"] != tt.want {
			t.Errorf("%s: status %d, body %s, want result %v", tt.body, rec.Code, rec.Body, tt.want)
		}
	}
}

func TestCalculateErrors(t *testing.T) {
	h := newHandler(t)
	tests := []struct {
		name, contentType, body string
		status                  int
		errMsg                  string
	}{
		{"division by zero", "application/json", `{"left":1,"operator":"/","right":0}`, 422, "division by zero"},
		{"overflow", "application/json", `{"left":1e308,"operator":"*","right":10}`, 422, "result out of range"},
		{"unsupported operator", "application/json", `{"left":1,"operator":"&","right":2}`, 400, "unsupported operator"},
		{"malformed JSON", "application/json", `{"left":1,`, 400, "invalid JSON request body"},
		{"unknown field", "application/json", `{"left":1,"operator":"+","right":2,"x":1}`, 400, "invalid JSON request body"},
		{"missing field", "application/json", `{"left":1,"operator":"+"}`, 400, "fields left, operator and right are required"},
		{"trailing data", "application/json", `{"left":1,"operator":"+","right":2} {}`, 400, "request body must contain a single JSON object"},
		{"wrong content type", "text/plain", `{"left":1,"operator":"+","right":2}`, 415, "content type must be application/json"},
		{"body too large", "application/json", `{"pad":"` + strings.Repeat("x", maxBodyBytes) + `"}`, 413, "request body too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, m := do(t, h, http.MethodPost, "/api/calculate", tt.contentType, tt.body)
			if rec.Code != tt.status || m["error"] != tt.errMsg {
				t.Fatalf("status %d body %s, want %d %q", rec.Code, rec.Body, tt.status, tt.errMsg)
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	rec, _ := do(t, newHandler(t), http.MethodGet, "/api/calculate", "", "")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
		t.Fatalf("status %d Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestProbesAndVersion(t *testing.T) {
	h := newHandler(t)
	for path, want := range map[string]string{"/health": "ok", "/healthz": "ok", "/readyz": "ready"} {
		if rec, m := do(t, h, http.MethodGet, path, "", ""); rec.Code != 200 || m["status"] != want {
			t.Errorf("GET %s: %d %s", path, rec.Code, rec.Body)
		}
	}
	rec, m := do(t, h, http.MethodGet, "/version", "", "")
	if rec.Code != 200 || m["name"] != serviceName || m["version"] != buildinfo.Version {
		t.Fatalf("GET /version: %d %s", rec.Code, rec.Body)
	}
}

func TestNoUI(t *testing.T) {
	if rec, _ := do(t, newHandler(t), http.MethodGet, "/", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET / = %d, want 404 (backend has no UI)", rec.Code)
	}
}

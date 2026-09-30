// Command frontend serves the HelloCalc UI and forwards /api/* to the backend
// service at BACKEND_URL (e.g. http://backend:8080, resolved by service name).
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/app"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/buildinfo"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/httpx"
)

// serviceName identifies this service in logs, /version and deploy.yaml.
const serviceName = "hellocalc-frontend"

// Backend call limits.
const (
	dialTimeout           = 3 * time.Second
	responseHeaderTimeout = 10 * time.Second
	probeTimeout          = 2 * time.Second
	maxVersionBytes       = 4 << 10
)

//go:embed web
var webFS embed.FS

func main() {
	app.Main(serviceName, build)
}

type frontend struct {
	backend *url.URL
	client  *http.Client
	logger  *slog.Logger
	info    buildinfo.Info
}

func build(getenv func(string) string, logger *slog.Logger, info buildinfo.Info) (*http.ServeMux, *httpx.Probes, error) {
	backend, err := parseBackendURL(getenv("BACKEND_URL"))
	if err != nil {
		return nil, nil, err
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
		ResponseHeaderTimeout: responseHeaderTimeout,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
	}
	f := &frontend{
		backend: backend,
		client:  &http.Client{Transport: transport},
		logger:  logger,
		info:    info,
	}
	logger.Info("backend configured", slog.String("backend_url", backend.String()))

	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, nil, err
	}

	probes := httpx.NewProbes(f.checkBackend)
	mux := http.NewServeMux()
	probes.Register(mux)
	mux.HandleFunc("GET /version", f.handleVersion)
	mux.Handle("/api/", f.proxy(transport))
	registerStatic(mux, assets)
	return mux, probes, nil
}

// parseBackendURL validates BACKEND_URL: an absolute http(s) URL without
// query or fragment. A path prefix is allowed.
func parseBackendURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("BACKEND_URL is required (e.g. http://backend:8080)")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid BACKEND_URL %q: must be an absolute http(s) URL such as http://backend:8080", raw)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u, nil
}

// proxy forwards /api/* to the backend, propagating the request ID so one
// X-Request-ID appears in both services' logs.
func (f *frontend) proxy(transport http.RoundTripper) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(f.backend)
			pr.SetXForwarded()
			pr.Out.Header.Set(httpx.RequestIDHeader, httpx.RequestID(pr.In.Context()))
		},
		Transport: transport,
		// The frontend already set these; drop the backend's copies so the
		// client sees each header once.
		ModifyResponse: func(res *http.Response) error {
			res.Header.Del(httpx.RequestIDHeader)
			for k := range httpx.SecurityHeaders {
				res.Header.Del(k)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			f.logger.WarnContext(r.Context(), "backend request failed",
				slog.String("path", r.URL.Path),
				slog.String("error", err.Error()),
				slog.String("request_id", httpx.RequestID(r.Context())),
			)
			httpx.WriteError(w, http.StatusBadGateway, "backend unavailable")
		},
	}
}

// backendGet performs a GET against the backend with the probe timeout.
func (f *frontend) backendGet(ctx context.Context, path string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.backend.String()+path, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	if id := httpx.RequestID(ctx); id != "" {
		req.Header.Set(httpx.RequestIDHeader, id)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = cancelOnClose{resp.Body, cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// checkBackend makes frontend readiness follow backend readiness, so a
// deployment only receives traffic once service-to-service wiring works.
func (f *frontend) checkBackend(ctx context.Context) error {
	resp, err := f.backendGet(ctx, "/readyz")
	if err != nil {
		f.logger.WarnContext(ctx, "backend readiness check failed", slog.String("error", err.Error()))
		return errors.New("backend unreachable")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxVersionBytes))
	if resp.StatusCode != http.StatusOK {
		f.logger.WarnContext(ctx, "backend not ready", slog.Int("status", resp.StatusCode))
		return errors.New("backend not ready")
	}
	return nil
}

type versionResponse struct {
	buildinfo.Info
	Backend json.RawMessage `json:"backend"`
}

// handleVersion reports this service's build and the backend's /version, so
// one request verifies which artifacts of both services are deployed.
func (f *frontend) handleVersion(w http.ResponseWriter, r *http.Request) {
	out := versionResponse{Info: f.info, Backend: json.RawMessage("null")}
	resp, err := f.backendGet(r.Context(), "/version")
	if err != nil {
		f.logger.WarnContext(r.Context(), "backend version lookup failed", slog.String("error", err.Error()))
	} else {
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxVersionBytes))
		if err == nil && resp.StatusCode == http.StatusOK && json.Valid(body) {
			out.Backend = json.RawMessage(body)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// registerStatic serves every embedded asset at its own path, with
// index.html at "/". Only exact paths are registered so unknown paths 404.
func registerStatic(mux *http.ServeMux, assets fs.FS) {
	entries, err := fs.ReadDir(assets, ".")
	if err != nil {
		panic("frontend: reading embedded assets: " + err.Error())
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		pattern := "GET /" + name
		if name == "index.html" {
			pattern = "GET /{$}"
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, assets, name)
		})
	}
}

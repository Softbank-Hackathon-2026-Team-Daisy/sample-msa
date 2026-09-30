// Package app is the common entrypoint for every service: configuration,
// logging, signal handling and the healthcheck/version subcommands.
//
//	<service>              start the server (configured via environment)
//	<service> healthcheck  probe /health on the local server; exit 0 if healthy
//	<service> version      print build metadata as JSON
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/buildinfo"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/config"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/httpx"
)

// BuildFunc builds a service's routes. getenv reads service-specific
// environment variables. The returned handler is wrapped with the standard
// middleware by Main.
type BuildFunc func(getenv func(string) string, logger *slog.Logger, info buildinfo.Info) (*http.ServeMux, *httpx.Probes, error)

// Main runs the named service and exits the process on error.
func Main(name string, build BuildFunc) {
	if err := run(name, build, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

func run(name string, build BuildFunc, args []string) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	info := buildinfo.Get(name)

	switch {
	case len(args) == 1 && args[0] == "healthcheck":
		return healthcheck(cfg)
	case len(args) == 1 && args[0] == "version":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	case len(args) != 0:
		return fmt.Errorf("unknown arguments %q (usage: %s [healthcheck|version])", args, name)
	}

	logger := httpx.NewLogger(os.Stdout, cfg.LogLevel, name)
	mux, probes, err := build(os.Getenv, logger, info)
	if err != nil {
		return err
	}
	logger.Info("starting",
		slog.String("version", info.Version),
		slog.String("commit", info.Commit),
		slog.String("build_time", info.BuildTime),
		slog.String("go_version", info.GoVersion),
		slog.String("addr", cfg.Addr()),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// After the first signal, restore default handling so a second signal
	// terminates immediately.
	context.AfterFunc(ctx, stop)

	ln, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		logger.Error("listen failed", slog.String("addr", cfg.Addr()), slog.String("error", err.Error()))
		return err
	}
	if err := httpx.Serve(ctx, ln, httpx.Wrap(logger, mux), cfg.ShutdownTimeout, logger, probes.SetReady); err != nil {
		logger.Error("server error", slog.String("error", err.Error()))
		return err
	}
	return nil
}

// healthcheck lets minimal images without a shell or curl run a container
// health check against the local server.
func healthcheck(cfg config.Config) error {
	host := cfg.Host
	switch host {
	case "0.0.0.0", "":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	}
	url := "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Port)) + "/health"

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s returned %d", url, resp.StatusCode)
	}
	return nil
}

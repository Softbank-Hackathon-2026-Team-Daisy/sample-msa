package config

import (
	"log/slog"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 8080 || cfg.LogLevel != slog.LevelInfo || cfg.ShutdownTimeout != 15*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if got := cfg.Addr(); got != "0.0.0.0:8080" {
		t.Fatalf("Addr() = %q, want 0.0.0.0:8080", got)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"HOST":             "::",
		"PORT":             "9000",
		"LOG_LEVEL":        "DEBUG",
		"SHUTDOWN_TIMEOUT": "3s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9000 || cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if got := cfg.Addr(); got != "[::]:9000" {
		t.Fatalf("Addr() = %q, want [::]:9000", got)
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := map[string]map[string]string{
		"non-numeric port":  {"PORT": "http"},
		"port zero":         {"PORT": "0"},
		"port too large":    {"PORT": "70000"},
		"unknown log level": {"LOG_LEVEL": "verbose"},
		"bad timeout":       {"SHUTDOWN_TIMEOUT": "soon"},
		"negative timeout":  {"SHUTDOWN_TIMEOUT": "-1s"},
	}
	for name, vars := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(vars)); err == nil {
				t.Fatalf("expected error for %v", vars)
			}
		})
	}
}

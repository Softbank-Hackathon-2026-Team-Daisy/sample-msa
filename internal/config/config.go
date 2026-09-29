// Package config reads HelloCalc runtime configuration from environment
// variables. No configuration files are used.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

// Config is the runtime configuration.
type Config struct {
	Host            string
	Port            int
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

// Defaults applied when a variable is unset or empty.
const (
	DefaultHost            = "0.0.0.0"
	DefaultPort            = 8080
	DefaultLogLevel        = "info"
	DefaultShutdownTimeout = 15 * time.Second
)

// Addr returns the listen address in host:port form.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// Load builds a Config from the given environment lookup function
// (typically os.Getenv).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Host:            DefaultHost,
		Port:            DefaultPort,
		ShutdownTimeout: DefaultShutdownTimeout,
	}

	if v := strings.TrimSpace(getenv("HOST")); v != "" {
		cfg.Host = v
	}

	if v := strings.TrimSpace(getenv("PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("invalid PORT %q: must be an integer between 1 and 65535", v)
		}
		cfg.Port = port
	}

	level := DefaultLogLevel
	if v := strings.TrimSpace(getenv("LOG_LEVEL")); v != "" {
		level = v
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return Config{}, fmt.Errorf("invalid LOG_LEVEL %q: must be debug, info, warn or error", level)
	}

	if v := strings.TrimSpace(getenv("SHUTDOWN_TIMEOUT")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("invalid SHUTDOWN_TIMEOUT %q: must be a positive duration such as 15s", v)
		}
		cfg.ShutdownTimeout = d
	}

	return cfg, nil
}

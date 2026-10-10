// Package config loads configuration from environment variables
// (ARCHITECTURE §7).
package config

import (
	"errors"
	"io"
	"log/slog"
	"time"
)

// Config is the validated configuration.
type Config struct {
	Token         string
	AllowedUsers  map[int64]bool
	DefaultTZ     *time.Location
	DefaultHour   int
	DefaultMinute int
	DBPath        string
	LogLevel      slog.Level
}

var errNotImplemented = errors.New("config: not implemented")

// Load reads configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	return Config{}, errNotImplemented // TODO(#2)
}

// NewLogger returns a text slog.Logger writing to w at cfg.LogLevel.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

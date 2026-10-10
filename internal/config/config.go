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
	LogLevel      slog.Level

	// DBURL selects the store provider: TODO_DB_URL, else TODO_DB_PATH
	// (deprecated, as sqlite:<path>), else sqlite://$STATE_DIRECTORY/todo.db,
	// else sqlite:todo.db.
	DBURL string
	// NodeID names this process for leases and claims (TODO_NODE_ID,
	// default os.Hostname()).
	NodeID string
	// Mode is "polling" (default) or "webhook" (v0.2) — TODO_MODE.
	Mode string
	// HTTPAddr is the listen address for the webhook and /healthz
	// (TODO_HTTP_ADDR, e.g. 127.0.0.1:8081); required in webhook mode.
	HTTPAddr string
	// WebhookURL is the public https URL Telegram posts to (TODO_WEBHOOK_URL).
	WebhookURL string
	// WebhookSecret is checked against X-Telegram-Bot-Api-Secret-Token
	// (TODO_WEBHOOK_SECRET, 16-256 chars of [A-Za-z0-9_-]).
	WebhookSecret string
	// LeaseTTL is the leader lease duration (TODO_LEASE_TTL, default 30s).
	LeaseTTL time.Duration
	// ClaimTTL is how long a node reserves a due reminder (TODO_CLAIM_TTL,
	// default 2m).
	ClaimTTL time.Duration
	// SchedMaxSleep caps the scheduler's sleep (TODO_SCHED_MAX_SLEEP,
	// default 5m; ~30s recommended with several nodes).
	SchedMaxSleep time.Duration
}

// Modes.
const (
	ModePolling = "polling"
	ModeWebhook = "webhook"
)

var errNotImplemented = errors.New("config: not implemented")

// Load reads configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	return Config{}, errNotImplemented // TODO(#2)
}

// NewLogger returns a text slog.Logger writing to w at cfg.LogLevel.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// Package config loads configuration from environment variables
// (ARCHITECTURE §7).
package config

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
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
	// default 2m, at least MinClaimTTL).
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

// hostname is os.Hostname; tests replace it.
var hostname = os.Hostname

var webhookSecretRE = regexp.MustCompile(`^[A-Za-z0-9_-]{16,256}$`)

// Load reads configuration through getenv (os.Getenv in production). It
// reports every problem at once. Error messages name the variable and the
// rule but never echo the token, a URL's userinfo or the webhook secret.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }

	c := Config{}

	if c.Token = get("TELEGRAM_TOKEN"); c.Token == "" {
		bad("TELEGRAM_TOKEN is required")
	}

	c.AllowedUsers = map[int64]bool{}
	if raw := get("TODO_ALLOWED_USERS"); raw == "" {
		bad("TODO_ALLOWED_USERS is required (comma-separated user ids)")
	} else {
		for _, part := range strings.Split(raw, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(i18n.NormalizeDigits(part)), 10, 64)
			// Telegram user ids are positive; 0 and negatives (chat ids) are not users.
			if err != nil || id <= 0 {
				bad("TODO_ALLOWED_USERS: entry %q is not a positive integer user id", part)
				continue
			}
			c.AllowedUsers[id] = true
		}
	}

	c.DefaultTZ = time.UTC
	if tz := get("TODO_DEFAULT_TZ"); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			bad("TODO_DEFAULT_TZ: unknown time zone %q", tz)
		} else {
			c.DefaultTZ = loc
		}
	}

	c.DefaultHour, c.DefaultMinute = 9, 0
	if t := get("TODO_DEFAULT_TIME"); t != "" {
		h, m, ok := parseHHMM(i18n.NormalizeDigits(t))
		if !ok {
			bad("TODO_DEFAULT_TIME: %q is not a valid HH:MM time", t)
		} else {
			c.DefaultHour, c.DefaultMinute = h, m
		}
	}

	c.LogLevel = slog.LevelInfo
	if l := get("TODO_LOG_LEVEL"); l != "" {
		switch strings.ToLower(l) {
		case "debug", "info", "warn", "error":
			_ = c.LogLevel.UnmarshalText([]byte(l))
		default:
			bad("TODO_LOG_LEVEL: %q is not one of debug, info, warn, error", l)
		}
	}

	var dberr error
	c.DBURL, dberr = dbURL(get)
	if dberr != nil {
		errs = append(errs, dberr)
	} else if u, err := url.Parse(c.DBURL); err != nil || u.Scheme == "" {
		// Never include the raw value: it may carry a password.
		bad("TODO_DB_URL: not a valid URL with a scheme (e.g. sqlite:///var/lib/todo-bot/todo.db)")
	} else if u.Scheme == "sqlite" && u.Host != "" {
		bad("TODO_DB_URL: sqlite URLs take no host; use sqlite:///abs/path or sqlite:rel/path")
	}

	c.NodeID = get("TODO_NODE_ID")
	if c.NodeID == "" {
		h, err := hostname()
		if err != nil || h == "" {
			bad("TODO_NODE_ID is unset and the hostname is unavailable")
		}
		c.NodeID = h
	}

	c.Mode = ModePolling
	if m := get("TODO_MODE"); m != "" {
		c.Mode = m
	}
	if c.Mode != ModePolling && c.Mode != ModeWebhook {
		bad("TODO_MODE: %q is not %q or %q", c.Mode, ModePolling, ModeWebhook)
	}

	c.HTTPAddr = get("TODO_HTTP_ADDR")
	c.WebhookURL = get("TODO_WEBHOOK_URL")
	c.WebhookSecret = get("TODO_WEBHOOK_SECRET")
	if c.Mode == ModeWebhook {
		if u, err := url.Parse(c.WebhookURL); c.WebhookURL == "" || err != nil || u.Scheme != "https" || u.Host == "" {
			bad("TODO_WEBHOOK_URL: an https URL with a host is required in webhook mode")
		}
		if !webhookSecretRE.MatchString(c.WebhookSecret) {
			bad("TODO_WEBHOOK_SECRET: 16-256 characters from [A-Za-z0-9_-] required in webhook mode")
		}
		if _, port, err := net.SplitHostPort(c.HTTPAddr); err != nil || port == "" {
			bad("TODO_HTTP_ADDR: a host:port listen address (e.g. 127.0.0.1:8081) is required in webhook mode")
		}
	}

	c.LeaseTTL = duration(get, "TODO_LEASE_TTL", 30*time.Second, bad)
	c.ClaimTTL = duration(get, "TODO_CLAIM_TTL", 2*time.Minute, bad)
	if c.ClaimTTL > 0 && c.ClaimTTL < MinClaimTTL {
		bad("TODO_CLAIM_TTL: %q is too short; it must be at least %s", get("TODO_CLAIM_TTL"), MinClaimTTL)
	}
	c.SchedMaxSleep = duration(get, "TODO_SCHED_MAX_SLEEP", 5*time.Minute, bad)

	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return c, nil
}

// MinClaimTTL is the smallest accepted TODO_CLAIM_TTL. A shorter claim leaves
// the scheduler's send deadline margin too small to cover the claim expiry
// (rounded down to the second) plus the 1 s clock skew that ARCHITECTURE §9
// allows.
const MinClaimTTL = 20 * time.Second

// dbURL applies the TODO_DB_URL fallback chain.
func dbURL(get func(string) string) (string, error) {
	if v := get("TODO_DB_URL"); v != "" {
		return v, nil
	}
	p := get("TODO_DB_PATH")
	if p == "" {
		// The first entry of the list; an empty one falls through.
		first, _, _ := strings.Cut(get("STATE_DIRECTORY"), ":")
		if first != "" {
			p = path.Join(first, "todo.db")
		}
	}
	if p == "" {
		p = "todo.db"
	}
	return sqliteURL(p)
}

// sqliteURL builds a sqlite URL from a file path: sqlite:///abs or sqlite:rel.
// A relative path goes into Opaque unescaped, so characters that would change
// the URL's meaning are rejected.
func sqliteURL(p string) (string, error) {
	if strings.HasPrefix(p, "/") {
		u := url.URL{Scheme: "sqlite", Path: p}
		return u.String(), nil
	}
	if strings.ContainsAny(p, "?#%") || strings.IndexFunc(p, unicode.IsSpace) >= 0 {
		return "", errors.New("use an absolute path for TODO_DB_PATH/STATE_DIRECTORY with special characters")
	}
	u := url.URL{Scheme: "sqlite", Opaque: p}
	return u.String(), nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseHHMM(s string) (h, m int, ok bool) {
	hs, ms, found := strings.Cut(s, ":")
	if !found || len(hs) == 0 || len(hs) > 2 || len(ms) != 2 || !isDigits(hs) || !isDigits(ms) {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(hs)
	m, err2 := strconv.Atoi(ms)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func duration(get func(string) string, key string, def time.Duration, bad func(string, ...any)) time.Duration {
	raw := get(key)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		bad("%s: %q is not a positive duration (e.g. 30s, 2m)", key, raw)
		return def
	}
	return d
}

// NewLogger returns a text slog.Logger writing to w at cfg.LogLevel.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

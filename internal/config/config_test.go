package config

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func base(extra map[string]string) map[string]string {
	m := map[string]string{
		"TELEGRAM_TOKEN":     "123:secrettoken",
		"TODO_ALLOWED_USERS": "11, 22",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

const goodSecret = "abcdEFGH_0123-456789"

func fakeHost(t *testing.T, name string, err error) {
	t.Helper()
	old := hostname
	hostname = func() (string, error) { return name, err }
	t.Cleanup(func() { hostname = old })
}

func TestLoadDefaults(t *testing.T) {
	fakeHost(t, "vps00", nil)
	c, err := Load(env(base(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "123:secrettoken" || !c.AllowedUsers[11] || !c.AllowedUsers[22] || len(c.AllowedUsers) != 2 {
		t.Errorf("token/users: %+v", c)
	}
	if c.DefaultTZ.String() != "UTC" || c.DefaultHour != 9 || c.DefaultMinute != 0 {
		t.Errorf("tz/time: %v %d:%d", c.DefaultTZ, c.DefaultHour, c.DefaultMinute)
	}
	if c.LogLevel != slog.LevelInfo || c.Mode != ModePolling || c.NodeID != "vps00" {
		t.Errorf("level/mode/node: %v %q %q", c.LogLevel, c.Mode, c.NodeID)
	}
	if c.DBURL != "sqlite:todo.db" && c.DBURL != "sqlite://todo.db" {
		t.Errorf("DBURL = %q", c.DBURL)
	}
	if c.LeaseTTL != 30*time.Second || c.ClaimTTL != 2*time.Minute || c.SchedMaxSleep != 5*time.Minute {
		t.Errorf("durations: %v %v %v", c.LeaseTTL, c.ClaimTTL, c.SchedMaxSleep)
	}
}

func TestLoadValues(t *testing.T) {
	fakeHost(t, "h", nil)
	c, err := Load(env(base(map[string]string{
		"TODO_DEFAULT_TZ":      "Europe/Berlin",
		"TODO_DEFAULT_TIME":    "۰۷:۳۰",
		"TODO_LOG_LEVEL":       "debug",
		"TODO_NODE_ID":         "n1",
		"TODO_LEASE_TTL":       "10s",
		"TODO_CLAIM_TTL":       "1m",
		"TODO_SCHED_MAX_SLEEP": "30s",
		"TODO_ALLOWED_USERS":   "5,-6,7",
	})))
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultTZ.String() != "Europe/Berlin" || c.DefaultHour != 7 || c.DefaultMinute != 30 {
		t.Errorf("tz/time: %v %d:%d", c.DefaultTZ, c.DefaultHour, c.DefaultMinute)
	}
	if c.LogLevel != slog.LevelDebug || c.NodeID != "n1" || c.LeaseTTL != 10*time.Second ||
		c.ClaimTTL != time.Minute || c.SchedMaxSleep != 30*time.Second || !c.AllowedUsers[-6] {
		t.Errorf("%+v", c)
	}
}

func TestDBURL(t *testing.T) {
	fakeHost(t, "h", nil)
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"explicit", map[string]string{"TODO_DB_URL": "sqlite:///var/lib/x.db", "TODO_DB_PATH": "/ignored", "STATE_DIRECTORY": "/s"}, "sqlite:///var/lib/x.db"},
		{"path alias abs", map[string]string{"TODO_DB_PATH": "/data/todo.db", "STATE_DIRECTORY": "/s"}, "sqlite:///data/todo.db"},
		{"state dir", map[string]string{"STATE_DIRECTORY": "/var/lib/todo-bot"}, "sqlite:///var/lib/todo-bot/todo.db"},
		{"state dir list", map[string]string{"STATE_DIRECTORY": "/a:/b"}, "sqlite:///a/todo.db"},
		{"default", nil, "sqlite:todo.db"},
		{"path alias rel", map[string]string{"TODO_DB_PATH": "data/x.db"}, "sqlite:data/x.db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Load(env(base(tt.env)))
			if err != nil {
				t.Fatal(err)
			}
			if c.DBURL != tt.want {
				t.Errorf("DBURL = %q, want %q", c.DBURL, tt.want)
			}
		})
	}
}

func TestHostnameError(t *testing.T) {
	fakeHost(t, "", errors.New("boom"))
	if _, err := Load(env(base(nil))); err == nil {
		t.Error("want error when hostname fails and no TODO_NODE_ID")
	}
	if _, err := Load(env(base(map[string]string{"TODO_NODE_ID": "x"}))); err != nil {
		t.Errorf("explicit node id should not need hostname: %v", err)
	}
}

func TestWebhookValid(t *testing.T) {
	fakeHost(t, "h", nil)
	c, err := Load(env(base(map[string]string{
		"TODO_MODE":           "webhook",
		"TODO_WEBHOOK_URL":    "https://bot.example.com/hook",
		"TODO_WEBHOOK_SECRET": goodSecret,
		"TODO_HTTP_ADDR":      "127.0.0.1:8081",
	})))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != ModeWebhook || c.WebhookURL != "https://bot.example.com/hook" || c.WebhookSecret != goodSecret || c.HTTPAddr != "127.0.0.1:8081" {
		t.Errorf("%+v", c)
	}
}

func TestLoadErrors(t *testing.T) {
	fakeHost(t, "h", nil)
	wh := func(k, v string) map[string]string {
		m := map[string]string{
			"TODO_MODE":           "webhook",
			"TODO_WEBHOOK_URL":    "https://bot.example.com/hook",
			"TODO_WEBHOOK_SECRET": goodSecret,
			"TODO_HTTP_ADDR":      ":8081",
		}
		m[k] = v
		return m
	}
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"missing token", map[string]string{"TELEGRAM_TOKEN": ""}, []string{"TELEGRAM_TOKEN"}},
		{"missing users", map[string]string{"TODO_ALLOWED_USERS": ""}, []string{"TODO_ALLOWED_USERS"}},
		{"bad users", map[string]string{"TODO_ALLOWED_USERS": "1,abc"}, []string{"TODO_ALLOWED_USERS"}},
		{"empty user entry", map[string]string{"TODO_ALLOWED_USERS": "1,,2"}, []string{"TODO_ALLOWED_USERS"}},
		{"bad tz", map[string]string{"TODO_DEFAULT_TZ": "Mars/Base"}, []string{"TODO_DEFAULT_TZ"}},
		{"bad time", map[string]string{"TODO_DEFAULT_TIME": "25:00"}, []string{"TODO_DEFAULT_TIME"}},
		{"bad time fmt", map[string]string{"TODO_DEFAULT_TIME": "9"}, []string{"TODO_DEFAULT_TIME"}},
		{"bad level", map[string]string{"TODO_LOG_LEVEL": "loud"}, []string{"TODO_LOG_LEVEL"}},
		{"bad mode", map[string]string{"TODO_MODE": "carrier-pigeon"}, []string{"TODO_MODE"}},
		{"bad duration", map[string]string{"TODO_LEASE_TTL": "soon"}, []string{"TODO_LEASE_TTL"}},
		{"zero duration", map[string]string{"TODO_CLAIM_TTL": "0s"}, []string{"TODO_CLAIM_TTL"}},
		{"negative duration", map[string]string{"TODO_SCHED_MAX_SLEEP": "-1m"}, []string{"TODO_SCHED_MAX_SLEEP"}},
		{"bad db url", map[string]string{"TODO_DB_URL": "://nope"}, []string{"TODO_DB_URL"}},
		{"db url no scheme", map[string]string{"TODO_DB_URL": "todo.db"}, []string{"TODO_DB_URL"}},
		{"webhook http url", wh("TODO_WEBHOOK_URL", "http://bot.example.com/hook"), []string{"TODO_WEBHOOK_URL"}},
		{"webhook no url", wh("TODO_WEBHOOK_URL", ""), []string{"TODO_WEBHOOK_URL"}},
		{"webhook no host", wh("TODO_WEBHOOK_URL", "https:///x"), []string{"TODO_WEBHOOK_URL"}},
		{"webhook short secret", wh("TODO_WEBHOOK_SECRET", "short"), []string{"TODO_WEBHOOK_SECRET"}},
		{"webhook long secret", wh("TODO_WEBHOOK_SECRET", strings.Repeat("a", 257)), []string{"TODO_WEBHOOK_SECRET"}},
		{"webhook bad chars", wh("TODO_WEBHOOK_SECRET", "abcdefghijklmnop!?"), []string{"TODO_WEBHOOK_SECRET"}},
		{"webhook no addr", wh("TODO_HTTP_ADDR", ""), []string{"TODO_HTTP_ADDR"}},
		{"all at once", map[string]string{"TELEGRAM_TOKEN": "", "TODO_ALLOWED_USERS": "", "TODO_MODE": "x", "TODO_LEASE_TTL": "z"},
			[]string{"TELEGRAM_TOKEN", "TODO_ALLOWED_USERS", "TODO_MODE", "TODO_LEASE_TTL"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(base(tt.env)))
			if err == nil {
				t.Fatal("want error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}

func TestErrorsNeverLeakSecrets(t *testing.T) {
	fakeHost(t, "h", nil)
	const (
		token  = "999:TOKEN-SHOULD-NOT-LEAK"
		dbPass = "hunter2-db-pass"
		secret = "bad secret with spaces and SECRETVALUE"
	)
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"bad secret", map[string]string{"TODO_MODE": "webhook", "TODO_WEBHOOK_SECRET": secret,
			"TODO_WEBHOOK_URL": "http://x.example.com", "TODO_HTTP_ADDR": ""}},
		{"bad db url with password", map[string]string{"TODO_DB_URL": "postgres://u:" + dbPass + "@h:notaport/db"}},
		{"unparseable db url with password", map[string]string{"TODO_DB_URL": "postgres://u:" + dbPass + "@h/%zz"}},
		{"bad users with token set", map[string]string{"TELEGRAM_TOKEN": token, "TODO_ALLOWED_USERS": "x", "TODO_MODE": "bad"}},
		{"secret and bad webhook url", map[string]string{"TODO_MODE": "webhook", "TODO_WEBHOOK_SECRET": "SECRETVALUE",
			"TODO_WEBHOOK_URL": "http://u:" + dbPass + "@h/%zz"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(base(tt.env)))
			if err == nil {
				t.Fatal("want error")
			}
			msg := err.Error()
			for _, s := range []string{token, "TOKEN-SHOULD-NOT-LEAK", "123:secrettoken", dbPass, "SECRETVALUE", secret} {
				if strings.Contains(msg, s) {
					t.Errorf("error leaks %q: %s", s, msg)
				}
			}
		})
	}
}

func TestNewLogger(t *testing.T) {
	var b bytes.Buffer
	l := NewLogger(&b, slog.LevelWarn)
	l.Info("hidden")
	l.Warn("shown")
	out := b.String()
	if strings.Contains(out, "hidden") || !strings.Contains(out, "shown") {
		t.Errorf("output = %q", out)
	}
}

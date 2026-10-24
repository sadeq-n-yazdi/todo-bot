package config

import (
	"bytes"
	"errors"
	"log/slog"
	"net/netip"
	"slices"
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
	if c.DBURL != "sqlite:todo.db" {
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
		"TODO_LOG_LEVEL":       "DEBUG",
		"TODO_NODE_ID":         "n1",
		"TODO_LEASE_TTL":       "10s",
		"TODO_CLAIM_TTL":       "1m",
		"TODO_SCHED_MAX_SLEEP": "30s",
		"TODO_ALLOWED_USERS":   "5,6,7",
	})))
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultTZ.String() != "Europe/Berlin" || c.DefaultHour != 7 || c.DefaultMinute != 30 {
		t.Errorf("tz/time: %v %d:%d", c.DefaultTZ, c.DefaultHour, c.DefaultMinute)
	}
	if c.LogLevel != slog.LevelDebug || c.NodeID != "n1" || c.LeaseTTL != 10*time.Second ||
		c.ClaimTTL != time.Minute || c.SchedMaxSleep != 30*time.Second || !c.AllowedUsers[6] {
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
		{"state dir relative", map[string]string{"STATE_DIRECTORY": "state"}, "sqlite:state/todo.db"},
		{"state dir empty first", map[string]string{"STATE_DIRECTORY": ":/b"}, "sqlite:todo.db"},
		{"abs path special", map[string]string{"TODO_DB_PATH": "/a b/c?d.db"}, "sqlite:///a%20b/c%3Fd.db"},
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

func TestClaimTTLMinimumIsAccepted(t *testing.T) {
	fakeHost(t, "vps00", nil)
	c, err := Load(env(base(map[string]string{"TODO_CLAIM_TTL": "20s"})))
	if err != nil || c.ClaimTTL != 20*time.Second {
		t.Fatalf("20s: %v, %v", c.ClaimTTL, err)
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
		{"claim ttl below minimum", map[string]string{"TODO_CLAIM_TTL": "19s"}, []string{"TODO_CLAIM_TTL", "at least 20s"}},
		{"negative duration", map[string]string{"TODO_SCHED_MAX_SLEEP": "-1m"}, []string{"TODO_SCHED_MAX_SLEEP"}},
		{"zero user", map[string]string{"TODO_ALLOWED_USERS": "0"}, []string{"TODO_ALLOWED_USERS"}},
		{"negative user", map[string]string{"TODO_ALLOWED_USERS": "-5"}, []string{"TODO_ALLOWED_USERS"}},
		{"signed time", map[string]string{"TODO_DEFAULT_TIME": "+9:00"}, []string{"TODO_DEFAULT_TIME"}},
		{"signed minute", map[string]string{"TODO_DEFAULT_TIME": "9:+5"}, []string{"TODO_DEFAULT_TIME"}},
		{"level offset", map[string]string{"TODO_LOG_LEVEL": "info+9"}, []string{"TODO_LOG_LEVEL"}},
		{"rel path question", map[string]string{"TODO_DB_PATH": "x?y.db"}, []string{"special characters"}},
		{"rel path hash", map[string]string{"TODO_DB_PATH": "x#y.db"}, []string{"special characters"}},
		{"rel path percent", map[string]string{"TODO_DB_PATH": "x%y.db"}, []string{"special characters"}},
		{"rel path space", map[string]string{"TODO_DB_PATH": "x y.db"}, []string{"special characters"}},
		{"rel state dir space", map[string]string{"STATE_DIRECTORY": "a b"}, []string{"special characters"}},
		{"sqlite host", map[string]string{"TODO_DB_URL": "sqlite://host/x.db"}, []string{"TODO_DB_URL"}},
		{"webhook addr no port", wh("TODO_HTTP_ADDR", "zz"), []string{"TODO_HTTP_ADDR"}},
		{"webhook addr empty port", wh("TODO_HTTP_ADDR", "host:"), []string{"TODO_HTTP_ADDR"}},
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

func TestWebDefaults(t *testing.T) {
	fakeHost(t, "h", nil)
	c, err := Load(env(base(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if c.WebAddr != "" || c.WebOrigin != "" {
		t.Errorf("web should be off: %q %q", c.WebAddr, c.WebOrigin)
	}
	if c.WebSessionTTL != 720*time.Hour || c.WebSessionMax != 2160*time.Hour || c.WebMemberTTL != 10*time.Minute {
		t.Errorf("durations: %v %v %v", c.WebSessionTTL, c.WebSessionMax, c.WebMemberTTL)
	}
	want := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}
	if !slices.Equal(c.WebTrustedProxies, want) {
		t.Errorf("proxies = %v", c.WebTrustedProxies)
	}
}

func TestWebValues(t *testing.T) {
	fakeHost(t, "h", nil)
	c, err := Load(env(base(map[string]string{
		"TODO_WEB_ADDR":            " 127.0.0.1:8088 ",
		"TODO_WEB_ORIGIN":          "https://todo.sadeq.uk",
		"TODO_WEB_SESSION_TTL":     "1h",
		"TODO_WEB_SESSION_MAX":     "1h",
		"TODO_WEB_MEMBER_TTL":      "1h",
		"TODO_WEB_TRUSTED_PROXIES": "10.0.0.0/8, fd00::/8",
		"TODO_HTTP_ADDR":           "127.0.0.1:8081",
	})))
	if err != nil {
		t.Fatal(err)
	}
	if c.WebAddr != "127.0.0.1:8088" || c.WebOrigin != "https://todo.sadeq.uk" {
		t.Errorf("addr/origin: %q %q", c.WebAddr, c.WebOrigin)
	}
	if c.WebSessionTTL != time.Hour || c.WebSessionMax != time.Hour || c.WebMemberTTL != time.Hour {
		t.Errorf("durations: %v %v %v", c.WebSessionTTL, c.WebSessionMax, c.WebMemberTTL)
	}
	want := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}
	if !slices.Equal(c.WebTrustedProxies, want) {
		t.Errorf("proxies = %v", c.WebTrustedProxies)
	}
}

func TestWebIPv6Loopback(t *testing.T) {
	fakeHost(t, "h", nil)
	c, err := Load(env(base(map[string]string{"TODO_WEB_ADDR": "[::1]:8088", "TODO_WEB_ORIGIN": "https://a.example"})))
	if err != nil || c.WebAddr != "[::1]:8088" {
		t.Fatalf("%q %v", c.WebAddr, err)
	}
}

func TestWebOriginWithoutAddrIsAllowed(t *testing.T) {
	fakeHost(t, "h", nil)
	if _, err := Load(env(base(map[string]string{"TODO_WEB_ORIGIN": "https://todo.example.com"}))); err != nil {
		t.Fatal(err)
	}
}

func TestWebErrors(t *testing.T) {
	fakeHost(t, "h", nil)
	on := func(k, v string) map[string]string {
		m := map[string]string{"TODO_WEB_ADDR": "127.0.0.1:8088", "TODO_WEB_ORIGIN": "https://todo.example.com"}
		m[k] = v
		return m
	}
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"addr not loopback", on("TODO_WEB_ADDR", "10.0.0.5:8088"), []string{"TODO_WEB_ADDR", "loopback"}},
		{"addr all interfaces", on("TODO_WEB_ADDR", "0.0.0.0:8088"), []string{"TODO_WEB_ADDR", "loopback"}},
		{"addr no host", on("TODO_WEB_ADDR", ":8088"), []string{"TODO_WEB_ADDR"}},
		{"addr name", on("TODO_WEB_ADDR", "localhost:8088"), []string{"TODO_WEB_ADDR", "loopback"}},
		{"addr no port", on("TODO_WEB_ADDR", "127.0.0.1"), []string{"TODO_WEB_ADDR"}},
		{"addr bad port", on("TODO_WEB_ADDR", "127.0.0.1:99999"), []string{"TODO_WEB_ADDR"}},
		{"addr zero port", on("TODO_WEB_ADDR", "127.0.0.1:0"), []string{"TODO_WEB_ADDR"}},
		{"addr zone", on("TODO_WEB_ADDR", "[::1%lo]:8088"), []string{"TODO_WEB_ADDR"}},
		{"addr equals http addr", map[string]string{
			"TODO_WEB_ADDR": "127.0.0.1:8081", "TODO_WEB_ORIGIN": "https://a.example", "TODO_HTTP_ADDR": "127.0.0.1:8081",
		}, []string{"TODO_WEB_ADDR", "TODO_HTTP_ADDR"}},
		{"origin missing", map[string]string{"TODO_WEB_ADDR": "127.0.0.1:8088"}, []string{"TODO_WEB_ORIGIN"}},
		{"origin http", on("TODO_WEB_ORIGIN", "http://todo.example.com"), []string{"TODO_WEB_ORIGIN"}},
		{"origin path", on("TODO_WEB_ORIGIN", "https://todo.example.com/app"), []string{"TODO_WEB_ORIGIN"}},
		{"origin slash", on("TODO_WEB_ORIGIN", "https://todo.example.com/"), []string{"TODO_WEB_ORIGIN"}},
		{"origin query", on("TODO_WEB_ORIGIN", "https://todo.example.com?x=1"), []string{"TODO_WEB_ORIGIN"}},
		{"origin userinfo", on("TODO_WEB_ORIGIN", "https://u:pw@todo.example.com"), []string{"TODO_WEB_ORIGIN"}},
		{"origin no host", on("TODO_WEB_ORIGIN", "https://"), []string{"TODO_WEB_ORIGIN"}},
		{"session ttl low", on("TODO_WEB_SESSION_TTL", "59m"), []string{"TODO_WEB_SESSION_TTL"}},
		{"session ttl high", on("TODO_WEB_SESSION_TTL", "2161h"), []string{"TODO_WEB_SESSION_TTL"}},
		{"session ttl junk", on("TODO_WEB_SESSION_TTL", "long"), []string{"TODO_WEB_SESSION_TTL"}},
		{"session max below ttl", on("TODO_WEB_SESSION_MAX", "719h"), []string{"TODO_WEB_SESSION_MAX", "TODO_WEB_SESSION_TTL"}},
		{"member ttl low", on("TODO_WEB_MEMBER_TTL", "59s"), []string{"TODO_WEB_MEMBER_TTL"}},
		{"member ttl high", on("TODO_WEB_MEMBER_TTL", "61m"), []string{"TODO_WEB_MEMBER_TTL"}},
		{"proxies junk", on("TODO_WEB_TRUSTED_PROXIES", "127.0.0.1/32,nope"), []string{"TODO_WEB_TRUSTED_PROXIES"}},
		{"proxies empty entry", on("TODO_WEB_TRUSTED_PROXIES", "127.0.0.1/32,,::1/128"), []string{"TODO_WEB_TRUSTED_PROXIES"}},
		{"proxies bare ip", on("TODO_WEB_TRUSTED_PROXIES", "127.0.0.1"), []string{"TODO_WEB_TRUSTED_PROXIES"}},
		// The rules apply also when the web is off.
		{"ttl checked when web off", map[string]string{"TODO_WEB_MEMBER_TTL": "1s"}, []string{"TODO_WEB_MEMBER_TTL"}},
		{"origin checked when web off", map[string]string{"TODO_WEB_ORIGIN": "http://x.example"}, []string{"TODO_WEB_ORIGIN"}},
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

func TestWebErrorsNeverLeakOriginUserinfo(t *testing.T) {
	fakeHost(t, "h", nil)
	_, err := Load(env(base(map[string]string{"TODO_WEB_ORIGIN": "https://user:hunter2@x.example"})))
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("err = %v", err)
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

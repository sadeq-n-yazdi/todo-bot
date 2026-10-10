package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf is a goroutine-safe buffer.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func dbURL(dir string) string {
	return (&url.URL{Scheme: "sqlite", Path: filepath.Join(dir, "todo.db")}).String()
}

func TestVersion(t *testing.T) {
	var out, errb syncBuf
	if code := run(context.Background(), []string{"version"}, envOf(nil), &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "todo-bot "+version) {
		t.Fatalf("output %q", out.String())
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"backup"}, {"backup", "a", "b"}} {
		var out, errb syncBuf
		if code := run(context.Background(), args, envOf(nil), &out, &errb); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestMigrateAndBackup(t *testing.T) {
	dir := t.TempDir()
	env := envOf(map[string]string{"TODO_DB_URL": dbURL(dir)}) // no token needed
	var out, errb syncBuf
	if code := run(context.Background(), []string{"migrate"}, env, &out, &errb); code != 0 {
		t.Fatalf("migrate exit %d: %s", code, errb.String())
	}
	dest := filepath.Join(dir, "copy.db")
	if code := run(context.Background(), []string{"backup", dest}, env, &out, &errb); code != 0 {
		t.Fatalf("backup exit %d: %s", code, errb.String())
	}
	if fi, err := os.Stat(dest); err != nil || fi.Size() == 0 {
		t.Fatalf("backup file: %v", err)
	}
}

func TestWebhookModeRefused(t *testing.T) {
	env := envOf(map[string]string{
		"TELEGRAM_TOKEN": "tok", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": dbURL(t.TempDir()),
		"TODO_MODE": "webhook", "TODO_WEBHOOK_URL": "https://example.com/hook",
		"TODO_WEBHOOK_SECRET": "abcdefghijklmnopqrstuvwx", "TODO_HTTP_ADDR": "127.0.0.1:8081",
	})
	var out, errb syncBuf
	if code := run(context.Background(), nil, env, &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "not supported until v0.2") {
		t.Fatalf("stderr %q", errb.String())
	}
}

func TestErrorsDoNotLeakSecrets(t *testing.T) {
	const pw = "hunter2-secret"
	const tok = "123456:TOKEN-SECRET"
	cases := map[string]map[string]string{
		"bad config":     {"TELEGRAM_TOKEN": tok, "TODO_DB_URL": "postgres://u:" + pw + "@h/db", "TODO_ALLOWED_USERS": "x"},
		"unparsable url": {"TELEGRAM_TOKEN": tok, "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": "://u:" + pw + "@h"},
		"open error":     {"TELEGRAM_TOKEN": tok, "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": "postgres://u:" + pw + "@h/db"},
		"sqlite creds":   {"TELEGRAM_TOKEN": tok, "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": "sqlite://u:" + pw + "@/x.db"},
	}
	for name, env := range cases {
		for _, sub := range []string{"run", "migrate"} {
			var out, errb syncBuf
			code := run(context.Background(), []string{sub}, envOf(env), &out, &errb)
			if code == 0 {
				t.Errorf("%s/%s: exit 0, want failure", name, sub)
			}
			if s := errb.String() + out.String(); strings.Contains(s, pw) || strings.Contains(s, tok) {
				t.Errorf("%s/%s leaks a secret: %q", name, sub, s)
			}
		}
	}
}

// tgCall is one recorded Bot API call.
type tgCall struct {
	token, method, lang string
	at                  time.Time
}

// mockTelegram is an httptest Bot API that records calls.
type mockTelegram struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []tgCall
}

func newMockTelegram(t *testing.T) *mockTelegram {
	m := &mockTelegram{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/bot")
		token, method, _ := strings.Cut(rest, "/")
		_ = r.ParseMultipartForm(1 << 20)
		m.mu.Lock()
		m.calls = append(m.calls, tgCall{token, method, r.Form.Get("language_code"), time.Now()})
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":42,"is_bot":true,"first_name":"T","username":"TodoBot"}}`))
		case "getUpdates": // a short long-poll
			select {
			case <-r.Context().Done():
			case <-time.After(300 * time.Millisecond):
				_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
			}
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	t.Cleanup(m.srv.Close)
	return m
}

// first returns the time of the token's first call of method, if any.
func (m *mockTelegram) first(token, method string) (time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c.token == token && c.method == method {
			return c.at, true
		}
	}
	return time.Time{}, false
}

func (m *mockTelegram) sequence(token string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var seq []string
	for _, c := range m.calls {
		if c.token != token || c.method == "getUpdates" && len(seq) > 0 && seq[len(seq)-1] == "getUpdates" {
			continue
		}
		s := c.method
		if c.method == "setMyCommands" {
			s += ":" + c.lang
		}
		seq = append(seq, s)
	}
	return seq
}

func waitUntil(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type node struct {
	cancel context.CancelFunc
	done   chan struct{}
	code   int
	stderr *syncBuf
}

func startNode(t *testing.T, env map[string]string) *node {
	ctx, cancel := context.WithCancel(context.Background())
	n := &node{cancel: cancel, done: make(chan struct{}), stderr: &syncBuf{}}
	go func() {
		n.code = run(ctx, []string{"run"}, envOf(env), &syncBuf{}, n.stderr)
		close(n.done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-n.done:
		case <-time.After(15 * time.Second):
			t.Errorf("node did not stop: %s", n.stderr.String())
		}
	})
	return n
}

func TestLeaseHandoverOnShutdown(t *testing.T) {
	mock := newMockTelegram(t)
	old := tgServerURL
	tgServerURL = mock.srv.URL
	t.Cleanup(func() { tgServerURL = old })

	const ttl = 1500 * time.Millisecond
	db := dbURL(t.TempDir())
	envFor := func(token, node string) map[string]string {
		return map[string]string{
			"TELEGRAM_TOKEN": token, "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": db,
			"TODO_NODE_ID": node, "TODO_LEASE_TTL": ttl.String(),
		}
	}

	a := startNode(t, envFor("tokA", "node-a"))
	waitUntil(t, 5*time.Second, "node A to poll", func() bool { _, ok := mock.first("tokA", "getUpdates"); return ok })

	wantSeq := []string{"getMe", "deleteWebhook", "setMyCommands:", "setMyCommands:fa", "getUpdates"}
	if got := mock.sequence("tokA"); strings.Join(got, ",") != strings.Join(wantSeq, ",") {
		t.Errorf("leader call sequence %v, want %v", got, wantSeq)
	}

	b := startNode(t, envFor("tokB", "node-b"))
	waitUntil(t, 5*time.Second, "node B to start", func() bool { _, ok := mock.first("tokB", "getMe"); return ok })
	time.Sleep(ttl) // B has had several attempts and must not poll while A leads
	if _, ok := mock.first("tokB", "getUpdates"); ok {
		t.Fatal("node B polls while node A holds the lease")
	}

	cancelledAt := time.Now()
	a.cancel()
	<-a.done
	if code := a.code; code != 0 {
		t.Fatalf("node A exit %d: %s", code, a.stderr.String())
	}
	waitUntil(t, 5*time.Second, "node B to take over", func() bool { _, ok := mock.first("tokB", "getUpdates"); return ok })
	at, _ := mock.first("tokB", "getUpdates")
	// B retries every ttl/3; allow some slack for a loaded machine.
	if d := at.Sub(cancelledAt); d > ttl/3+750*time.Millisecond {
		t.Errorf("takeover took %v, want about ttl/3 = %v", d, ttl/3)
	}
	_ = b
}

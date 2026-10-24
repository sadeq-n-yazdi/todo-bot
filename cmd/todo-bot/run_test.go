package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/web"
	"github.com/sadeq-n-yazdi/todo-bot/migrations"
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
	ui := "\nweb: placeholder"
	if web.Embedded() {
		ui = "\nweb: embedded"
	}
	if first, _, _ := strings.Cut(out.String(), "\n"); !strings.HasPrefix(first, "todo-bot "+version+" (commit ") {
		t.Fatalf("first line %q changed", first)
	}
	for _, want := range []string{"todo-bot " + version, "(commit ", ", built ", ui} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output %q lacks %q", out.String(), want)
		}
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
	base := func(extra map[string]string) map[string]string {
		m := map[string]string{"TELEGRAM_TOKEN": tok, "TODO_ALLOWED_USERS": "1"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	closed := func() string { // a URL nobody listens on
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		u := "http://" + l.Addr().String()
		_ = l.Close()
		return u
	}
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	}))
	t.Cleanup(unauthorized.Close)

	cases := []struct {
		name, sub string
		env       map[string]string
		tgURL     string
		wantCode  int    // 0 = the run is stopped by its context
		want      string // must appear in stderr
	}{
		{"bad config", "run", base(map[string]string{"TODO_DB_URL": "postgres://u:" + pw + "@h/db", "TODO_ALLOWED_USERS": "x"}), "", 1, "TODO_ALLOWED_USERS"},
		{"bad scheme", "migrate", base(map[string]string{"TODO_DB_URL": "postgres://u:" + pw + "@h/db"}), "", 1, "no provider"},
		{"unparsable url", "run", base(map[string]string{"TODO_DB_URL": "://u:" + pw + "@h"}), "", 1, "TODO_DB_URL"},
		{"unparsable url", "migrate", base(map[string]string{"TODO_DB_URL": "://u:" + pw + "@h"}), "", 1, "TODO_DB_URL"},
		{"open error", "run", base(map[string]string{"TODO_DB_URL": "postgres://u:" + pw + "@h/db"}), "", 1, "no provider"},
		{"sqlite creds", "run", base(map[string]string{"TODO_DB_URL": "sqlite://u:" + pw + "@/x.db"}), "", 1, "xxxxx"},
		{"sqlite creds", "migrate", base(map[string]string{"TODO_DB_URL": "sqlite://u:" + pw + "@/x.db"}), "", 1, "xxxxx"},
		{"getMe refused", "run", base(map[string]string{"TODO_DB_URL": dbURL(t.TempDir())}), closed(), 0, "retrying"},
		{"getMe 401", "run", base(map[string]string{"TODO_DB_URL": dbURL(t.TempDir())}), unauthorized.URL, 1, "todo-bot: telegram"},
	}
	setTelegram(t, "")
	oldMin, oldMax := startBackoffMin, startBackoffMax
	startBackoffMin, startBackoffMax = 20*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { startBackoffMin, startBackoffMax = oldMin, oldMax })
	for _, c := range cases {
		t.Run(c.name+"/"+c.sub, func(t *testing.T) {
			tgServerURL = c.tgURL
			if c.tgURL != "" {
				// Migrate first so the short deadline below times startup,
				// not schema creation (slow under -race on CI runners).
				var discard syncBuf
				if code := run(context.Background(), []string{"migrate"}, envOf(c.env), &discard, &discard); code != 0 {
					t.Fatalf("migrate exit %d: %s", code, discard.String())
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			defer cancel()
			var out, errb syncBuf
			if code := run(ctx, []string{c.sub}, envOf(c.env), &out, &errb); code != c.wantCode {
				t.Errorf("exit %d, want %d; stderr: %s", code, c.wantCode, errb.String())
			}
			all := errb.String() + out.String()
			if !strings.Contains(all, c.want) {
				t.Errorf("output lacks %q: %q", c.want, all)
			}
			if strings.Contains(all, pw) || strings.Contains(all, tok) {
				t.Errorf("output leaks a secret: %q", all)
			}
		})
	}
}

func setTelegram(t *testing.T, u string) {
	old := tgServerURL
	tgServerURL = u
	t.Cleanup(func() { tgServerURL = old })
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

	getMeFails int // the first n getMe calls answer 500
	getMeCode  int // if non-zero, every getMe answers this status
	htmlFails  int // the first n getMe calls answer 502 with an HTML page
	retryAfter int // seconds, sent as parameters.retry_after with a 429
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
			m.mu.Lock()
			code := m.getMeCode
			if m.htmlFails > 0 {
				m.htmlFails--
				m.mu.Unlock()
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("<html><body>502 Bad Gateway</body></html>"))
				return
			}
			if m.getMeFails > 0 {
				m.getMeFails--
				code = http.StatusInternalServerError
			}
			m.mu.Unlock()
			if code != 0 {
				w.WriteHeader(code)
				params := ""
				if code == http.StatusTooManyRequests && m.retryAfter > 0 {
					params = `,"parameters":{"retry_after":` + strconv.Itoa(m.retryAfter) + `}`
				}
				_, _ = w.Write([]byte(`{"ok":false,"error_code":` + strconv.Itoa(code) + `,"description":"x"` + params + `}`))
				return
			}
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
	setTelegram(t, mock.srv.URL)

	const ttl = 1500 * time.Millisecond
	dir := t.TempDir()
	db := dbURL(dir)
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
	// The lease must have been released, not just expired: B may already hold it.
	holder, until := leaseRow(t, filepath.Join(dir, "todo.db"))
	if until != 0 && !strings.HasPrefix(holder, "node-b:") {
		t.Errorf("poller lease not released after A exited: holder %q held_until %d", holder, until)
	}
	waitUntil(t, 5*time.Second, "node B to take over", func() bool { _, ok := mock.first("tokB", "getUpdates"); return ok })
	at, _ := mock.first("tokB", "getUpdates")
	// B retries every ttl/3; allow some slack for a loaded machine.
	if d := at.Sub(cancelledAt); d > ttl/3+750*time.Millisecond {
		t.Errorf("takeover took %v, want about ttl/3 = %v", d, ttl/3)
	}
	b.cancel()
	<-b.done
	if b.code != 0 {
		t.Errorf("node B exit %d: %s", b.code, b.stderr.String())
	}
}

func leaseRow(t *testing.T, path string) (holder string, until int64) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow("SELECT holder, held_until FROM leases WHERE name = 'poller'").Scan(&holder, &until); err != nil {
		t.Fatal(err)
	}
	return holder, until
}

func TestStartupRetriesGetMe(t *testing.T) {
	mock := newMockTelegram(t)
	mock.getMeFails = 2
	setTelegram(t, mock.srv.URL)
	oldMin, oldMax := startBackoffMin, startBackoffMax
	startBackoffMin, startBackoffMax = 10*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { startBackoffMin, startBackoffMax = oldMin, oldMax })

	n := startNode(t, map[string]string{"TELEGRAM_TOKEN": "tokR", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": dbURL(t.TempDir()), "TODO_NODE_ID": "r"})
	waitUntil(t, 5*time.Second, "polling after retries", func() bool { _, ok := mock.first("tokR", "getUpdates"); return ok })
	if !strings.Contains(n.stderr.String(), "retrying") {
		t.Errorf("no retry logged: %s", n.stderr.String())
	}
}

func TestStartupUnauthorizedIsFatal(t *testing.T) {
	mock := newMockTelegram(t)
	mock.getMeCode = http.StatusUnauthorized
	setTelegram(t, mock.srv.URL)
	var out, errb syncBuf
	env := map[string]string{"TELEGRAM_TOKEN": "tokU", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": dbURL(t.TempDir())}
	if code := run(context.Background(), nil, envOf(env), &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, errb.String())
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.calls) != 1 {
		t.Errorf("getMe called %d times, want 1", len(mock.calls))
	}
}

// hangStore blocks AcquireLease (ignoring its context) and records Close.
type hangStore struct {
	store.Store
	release  chan struct{}
	closed   atomic.Bool
	acquires atomic.Int64  // AcquireLease calls running now
	released chan struct{} // closed by the first ReleaseLease call that returns
	relOnce  sync.Once
}

func (h *hangStore) AcquireLease(ctx context.Context, name, holder string, now time.Time, ttl time.Duration) (m model.Lease, err error) {
	h.acquires.Add(1)
	defer h.acquires.Add(-1)
	<-h.release
	return h.Store.AcquireLease(ctx, name, holder, now, ttl)
}

func (h *hangStore) ReleaseLease(ctx context.Context, name, holder string) error {
	defer h.relOnce.Do(func() { close(h.released) })
	return h.Store.ReleaseLease(ctx, name, holder)
}

func (h *hangStore) Close() error { h.closed.Store(true); return nil }

func TestShutdownTimeoutSkipsClose(t *testing.T) {
	mock := newMockTelegram(t)
	setTelegram(t, mock.srv.URL)
	real, err := store.Open(context.Background(), dbURL(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = real.Close() })
	hs := &hangStore{Store: real, release: make(chan struct{}), released: make(chan struct{})}
	// Registered after real.Close, so it runs first: unblock the hanging call,
	// then wait for the lease loop to finish its release, so that no goroutine
	// of run still uses the database when the test ends.
	t.Cleanup(func() {
		close(hs.release)
		select {
		case <-hs.released:
		case <-time.After(10 * time.Second):
			t.Error("lease loop did not stop after the hanging call was released")
		}
		waitUntil(t, 10*time.Second, "hanging calls to return", func() bool { return hs.acquires.Load() == 0 })
	})
	oldOpen, oldTO := openStore, shutdownTimeout
	openStore = func(context.Context, string) (store.Store, error) { return hs, nil }
	shutdownTimeout = 300 * time.Millisecond
	t.Cleanup(func() { openStore, shutdownTimeout = oldOpen, oldTO })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var errb syncBuf
	env := map[string]string{"TELEGRAM_TOKEN": "tokH", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": "sqlite:x.db"}
	go func() { done <- run(ctx, nil, envOf(env), &syncBuf{}, &errb) }()
	waitUntil(t, 5*time.Second, "node to start", func() bool { _, ok := mock.first("tokH", "getMe"); return ok })
	time.Sleep(100 * time.Millisecond) // let the lease loop enter its hanging call
	start := time.Now()
	cancel()
	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("exit %d, want 1", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not return within the shutdown bound")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("shutdown took %v", d)
	}
	if hs.closed.Load() {
		t.Error("store was closed although the lease loop had not stopped")
	}
	if !strings.Contains(errb.String(), "timed out") {
		t.Errorf("stderr %q", errb.String())
	}
}

func TestBackupMissingDatabaseFails(t *testing.T) {
	dir := t.TempDir()
	env := envOf(map[string]string{"TODO_DB_URL": dbURL(dir)})
	dest := filepath.Join(dir, "copy.db")
	var out, errb syncBuf
	if code := run(context.Background(), []string{"backup", dest}, env, &out, &errb); code != 1 {
		t.Fatalf("backup exit %d, want 1: %s", code, errb.String())
	}
	for _, p := range []string{filepath.Join(dir, "todo.db"), dest} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was created", p)
		}
	}
}

// Backup of a database at an older schema version must not migrate it.
func TestBackupDoesNotMigrate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.db")
	mfs, err := migrations.For("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	body, err := fs.ReadFile(mfs, "0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	dest := filepath.Join(dir, "copy.db")
	var out, errb syncBuf
	env := envOf(map[string]string{"TODO_DB_URL": dbURL(dir)})
	if code := run(context.Background(), []string{"backup", dest}, env, &out, &errb); code != 0 {
		t.Fatalf("backup exit %d: %s", code, errb.String())
	}
	for _, p := range []string{path, dest} {
		db, err := sql.Open("sqlite", "file:"+p)
		if err != nil {
			t.Fatal(err)
		}
		var uv, tables int
		if err := db.QueryRow("PRAGMA user_version").Scan(&uv); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'schema_migrations'").Scan(&tables); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		if uv != 1 || tables != 0 {
			t.Errorf("%s: user_version %d, schema_migrations tables %d; want 1 and 0", p, uv, tables)
		}
	}
}

func TestRetryWait(t *testing.T) {
	const d = 2 * time.Second
	cases := []struct {
		name  string
		err   error
		wait  time.Duration
		retry bool
	}{
		{"429 longer", &tg.APIError{Code: 429, RetryAfter: 10 * time.Second}, 10 * time.Second, true},
		{"429 shorter", &tg.APIError{Code: 429, RetryAfter: time.Second}, d, true},
		{"429 none", &tg.APIError{Code: 429}, d, true},
		{"500", &tg.APIError{Code: 500}, d, true},
		{"502 wrapped", fmt.Errorf("x: %w", &tg.APIError{Code: 502}), d, true},
		{"401", &tg.APIError{Code: 401}, 0, false},
		{"404", &tg.APIError{Code: 404}, 0, false},
		{"network", &net.OpError{Op: "dial", Err: errors.New("refused")}, d, true},
		{"url error", &url.Error{Op: "Get", URL: "x", Err: errors.New("boom")}, d, true},
		{"eof", io.ErrUnexpectedEOF, d, true},
		{"html body", fmt.Errorf("error decode response body for method getMe, <html>: %w", &json.SyntaxError{}), d, true},
		{"empty body", fmt.Errorf("x: %w", &json.UnmarshalTypeError{}), d, true},
		{"other", errors.New("tgbot: ServerURL must be an http(s) URL with a host"), 0, false},
	}
	for _, c := range cases {
		wait, retry := retryWait(c.err, d)
		if wait != c.wait || retry != c.retry {
			t.Errorf("%s: retryWait = (%v, %v), want (%v, %v)", c.name, wait, retry, c.wait, c.retry)
		}
	}
}

func TestStartupHonoursRetryAfter(t *testing.T) {
	mock := newMockTelegram(t)
	mock.getMeCode, mock.retryAfter = http.StatusTooManyRequests, 1
	setTelegram(t, mock.srv.URL)
	oldMin, oldMax := startBackoffMin, startBackoffMax
	startBackoffMin, startBackoffMax = 10*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { startBackoffMin, startBackoffMax = oldMin, oldMax })

	startNode(t, map[string]string{"TELEGRAM_TOKEN": "tokT", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": dbURL(t.TempDir()), "TODO_NODE_ID": "t"})
	waitUntil(t, 5*time.Second, "first getMe", func() bool { _, ok := mock.first("tokT", "getMe"); return ok })
	first, _ := mock.first("tokT", "getMe")
	waitUntil(t, 10*time.Second, "second getMe", func() bool {
		mock.mu.Lock()
		defer mock.mu.Unlock()
		n := 0
		for _, c := range mock.calls {
			if c.token == "tokT" && c.method == "getMe" {
				n++
			}
		}
		return n >= 2
	})
	mock.mu.Lock()
	var second time.Time
	n := 0
	for _, c := range mock.calls {
		if c.token == "tokT" && c.method == "getMe" {
			if n++; n == 2 {
				second = c.at
			}
		}
	}
	mock.mu.Unlock()
	// The backoff alone is 10ms; Telegram asked for 1s.
	if d := second.Sub(first); d < 900*time.Millisecond {
		t.Errorf("retried after %v, want at least the 1s RetryAfter", d)
	}
}

func TestStartupOtherErrorIsFatal(t *testing.T) {
	setTelegram(t, "ftp://example.invalid") // tgbot.New rejects it without a network call
	var out, errb syncBuf
	env := map[string]string{"TELEGRAM_TOKEN": "tokF", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": dbURL(t.TempDir())}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if code := run(ctx, nil, envOf(env), &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, errb.String())
	}
	if strings.Contains(errb.String(), "retrying") {
		t.Errorf("a non-transient error was retried: %s", errb.String())
	}
}

func TestStartupRetriesHTMLGatewayError(t *testing.T) {
	mock := newMockTelegram(t)
	mock.htmlFails = 2
	setTelegram(t, mock.srv.URL)
	oldMin, oldMax := startBackoffMin, startBackoffMax
	startBackoffMin, startBackoffMax = 10*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { startBackoffMin, startBackoffMax = oldMin, oldMax })

	n := startNode(t, map[string]string{"TELEGRAM_TOKEN": "tokG", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": dbURL(t.TempDir()), "TODO_NODE_ID": "g"})
	waitUntil(t, 5*time.Second, "polling after HTML 502s", func() bool { _, ok := mock.first("tokG", "getUpdates"); return ok })
	if !strings.Contains(n.stderr.String(), "retrying") {
		t.Errorf("no retry logged: %s", n.stderr.String())
	}
}

// mode=existing is for backup only: run and migrate must refuse it.
func TestExistingModeRefusedOutsideBackup(t *testing.T) {
	dir := t.TempDir()
	env := envOf(map[string]string{
		"TODO_DB_URL":    dbURL(dir) + "?mode=existing",
		"TELEGRAM_TOKEN": "tokE", "TODO_ALLOWED_USERS": "1",
	})
	for _, sub := range []string{"run", "migrate", "backup"} {
		var out, errb syncBuf
		args := []string{sub}
		if sub == "backup" {
			args = append(args, filepath.Join(dir, "copy.db"))
		}
		if code := run(context.Background(), args, env, &out, &errb); code != 1 {
			t.Errorf("%s: exit %d, want 1", sub, code)
		}
		if !strings.Contains(errb.String(), "query") {
			t.Errorf("%s: stderr %q does not say the query is refused", sub, errb.String())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "todo.db")); err == nil {
		t.Error("a database file was created")
	}
}

// webListener wraps listenWeb: it listens on a free loopback port instead
// of the configured address, and reports each call.
func webListener(t *testing.T) (calls *atomic.Int64, addrs chan string) {
	t.Helper()
	calls, addrs = &atomic.Int64{}, make(chan string, 4)
	old := listenWeb
	listenWeb = func(network, _ string) (net.Listener, error) {
		calls.Add(1)
		l, err := net.Listen(network, "127.0.0.1:0")
		if err == nil {
			addrs <- l.Addr().String()
		}
		return l, err
	}
	t.Cleanup(func() { listenWeb = old })
	return calls, addrs
}

func TestWebOffStartsNoListener(t *testing.T) {
	mock := newMockTelegram(t)
	setTelegram(t, mock.srv.URL)
	calls, _ := webListener(t)
	n := startNode(t, map[string]string{"TELEGRAM_TOKEN": "tokW0", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": dbURL(t.TempDir()), "TODO_NODE_ID": "w0"})
	waitUntil(t, 5*time.Second, "polling", func() bool { _, ok := mock.first("tokW0", "getUpdates"); return ok })
	n.cancel()
	<-n.done
	if n.code != 0 {
		t.Fatalf("exit %d: %s", n.code, n.stderr.String())
	}
	if calls.Load() != 0 {
		t.Error("a web listener was opened with TODO_WEB_ADDR empty")
	}
	if strings.Contains(n.stderr.String(), "web:") {
		t.Errorf("web log lines with the web off: %s", n.stderr.String())
	}
}

func insertSession(t *testing.T, db *sql.DB, id string, user int64) {
	t.Helper()
	far := time.Now().Add(24 * time.Hour).Unix()
	if _, err := db.Exec(`INSERT INTO web_sessions (id, user_id, kind, device, created_at, seen_at, expires_at, max_at)
		VALUES (?, ?, 'browser', '', ?, ?, ?, ?)`, id, user, far-100, far-100, far, far); err != nil {
		t.Fatal(err)
	}
}

func countSessions(t *testing.T, db *sql.DB, user int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM web_sessions WHERE user_id = ?", user).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWebStartsPrunesAndStops(t *testing.T) {
	mock := newMockTelegram(t)
	setTelegram(t, mock.srv.URL)
	calls, addrs := webListener(t)
	dir := t.TempDir()
	env := map[string]string{
		"TELEGRAM_TOKEN": "tokW1", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": dbURL(dir), "TODO_NODE_ID": "w1",
		"TODO_WEB_ADDR": "127.0.0.1:8088", "TODO_WEB_ORIGIN": "https://todo.example.com",
	}
	var discard syncBuf
	if code := run(context.Background(), []string{"migrate"}, envOf(env), &discard, &discard); code != 0 {
		t.Fatalf("migrate: %s", discard.String())
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "todo.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insertSession(t, db, strings.Repeat("a", 64), 1)   // allowed
	insertSession(t, db, strings.Repeat("b", 64), 999) // not allowed

	n := startNode(t, env)
	var addr string
	select {
	case addr = <-addrs:
	case <-time.After(10 * time.Second):
		t.Fatalf("the web did not start: %s", n.stderr.String())
	}
	get := func(path, host string) (int, string, error) {
		req, _ := http.NewRequest("GET", "http://"+addr+path, nil)
		if host != "" {
			req.Host = host
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), nil
	}
	if code, body, err := get("/healthz", ""); err != nil || code != 200 || body != "ok" {
		t.Fatalf("healthz: %d %q %v", code, body, err)
	}
	if code, body, err := get("/", "todo.example.com"); err != nil || code != 200 || !strings.Contains(strings.ToLower(body), "<!doctype html>") {
		t.Fatalf("/: %d %q %v", code, body, err)
	}
	if code, _, _ := get("/", ""); code != http.StatusMisdirectedRequest {
		t.Errorf("/ with the loopback host: %d", code)
	}
	if countSessions(t, db, 999) != 0 || countSessions(t, db, 1) != 1 {
		t.Errorf("prune at start: user 999 has %d sessions, user 1 has %d", countSessions(t, db, 999), countSessions(t, db, 1))
	}
	waitUntil(t, 5*time.Second, "polling", func() bool { _, ok := mock.first("tokW1", "getUpdates"); return ok })

	start := time.Now()
	n.cancel()
	<-n.done
	if n.code != 0 {
		t.Fatalf("exit %d: %s", n.code, n.stderr.String())
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("shutdown took %v", d)
	}
	if _, _, err := get("/healthz", ""); err == nil {
		t.Error("the web still answers after the run returned")
	}
	if calls.Load() != 1 {
		t.Errorf("%d listeners", calls.Load())
	}
}

func TestWebBindErrorStopsStart(t *testing.T) {
	mock := newMockTelegram(t)
	setTelegram(t, mock.srv.URL)
	old := listenWeb
	listenWeb = func(string, string) (net.Listener, error) { return nil, errors.New("address in use") }
	t.Cleanup(func() { listenWeb = old })
	var out, errb syncBuf
	env := map[string]string{"TELEGRAM_TOKEN": "tokW2", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": dbURL(t.TempDir()),
		"TODO_WEB_ADDR": "127.0.0.1:8088", "TODO_WEB_ORIGIN": "https://todo.example.com"}
	if code := run(context.Background(), nil, envOf(env), &out, &errb); code != 1 || !strings.Contains(errb.String(), "address in use") {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if _, ok := mock.first("tokW2", "getUpdates"); ok {
		t.Error("the bot polled although the web did not start")
	}
}

// webHangStore blocks ListGroupChats (the /readyz read) until released,
// whatever its context says, and records Close.
type webHangStore struct {
	store.Store
	release chan struct{}
	entered chan struct{}
	once    sync.Once
	closed  atomic.Bool
}

func (h *webHangStore) ListGroupChats(ctx context.Context, limit int) ([]model.Chat, error) {
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return h.Store.ListGroupChats(ctx, limit)
}

func (h *webHangStore) Close() error { h.closed.Store(true); return nil }

// A web request that does not end holds the shutdown only up to the one
// shutdownTimeout; then run gives up and leaves the database open.
func TestShutdownTimeoutStopsWeb(t *testing.T) {
	mock := newMockTelegram(t)
	setTelegram(t, mock.srv.URL)
	_, addrs := webListener(t)
	real, err := store.Open(context.Background(), dbURL(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = real.Close() })
	hs := &webHangStore{Store: real, release: make(chan struct{}), entered: make(chan struct{})}
	t.Cleanup(func() { close(hs.release) })
	oldOpen, oldTO := openStore, shutdownTimeout
	openStore = func(context.Context, string) (store.Store, error) { return hs, nil }
	shutdownTimeout = 300 * time.Millisecond
	t.Cleanup(func() { openStore, shutdownTimeout = oldOpen, oldTO })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var errb syncBuf
	env := map[string]string{"TELEGRAM_TOKEN": "tokW3", "TODO_ALLOWED_USERS": "1", "TODO_DB_URL": "sqlite:x.db",
		"TODO_WEB_ADDR": "127.0.0.1:8088", "TODO_WEB_ORIGIN": "https://todo.example.com"}
	go func() { done <- run(ctx, nil, envOf(env), &syncBuf{}, &errb) }()
	addr := <-addrs
	go func() {
		resp, err := http.Get("http://" + addr + "/readyz")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-hs.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the readyz request did not reach the store")
	}
	start := time.Now()
	cancel()
	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("exit %d, want 1", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not return within the shutdown bound")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("shutdown took %v", d)
	}
	if !strings.Contains(errb.String(), "timed out stopping the web server") {
		t.Errorf("stderr %q", errb.String())
	}
	if hs.closed.Load() {
		t.Error("the store was closed although a web request still ran")
	}
}

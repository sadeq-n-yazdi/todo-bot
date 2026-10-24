package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/config"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	_ "github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite" // registers the "sqlite" scheme
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
)

const (
	testOrigin = "https://todo.example.com"
	testHost   = "todo.example.com"
	idAnn      = 42 // userAnn of initdata_test.go, language fa
	idBob      = 43
	idEve      = 99 // not in the allowlist
	userBob    = `{"id":43,"first_name":"Bob","username":"bob","language_code":"en"}`
	userEve    = `{"id":99,"first_name":"Eve","username":"eve"}`
)

// syncBuf is a goroutine-safe buffer for the log.
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

type harness struct {
	t       *testing.T
	srv     *Server
	st      store.Store
	svc     *service.Svc
	clk     *clocktest.Fake
	send    *tgtest.FakeSender
	members *tgtest.FakeMembers
	log     *syncBuf
}

var testAssets = fstest.MapFS{
	"index.html":               {Data: []byte("<!doctype html><title>pwa</title>")},
	"tg/index.html":            {Data: []byte("<!doctype html><title>miniapp</title>")},
	"assets/app-1a2b3c.js":     {Data: []byte("console.log(1)")},
	"assets/app-1a2b3c.css":    {Data: []byte("body{}")},
	"icons/icon-192.png":       {Data: []byte("\x89PNG")},
	"manifest.webmanifest":     {Data: []byte(`{"name":"Todo"}`)},
	"sw.js":                    {Data: []byte("self.x=1")},
	"assets/sub/nested-ab.txt": {Data: []byte("n")},
}

func testConfig() config.Config {
	return config.Config{
		WebAddr:       "127.0.0.1:0",
		WebOrigin:     testOrigin,
		WebSessionTTL: 720 * time.Hour,
		WebSessionMax: 2160 * time.Hour,
		WebMemberTTL:  10 * time.Minute,
		WebTrustedProxies: []netip.Prefix{
			netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128"),
		},
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	u := (&url.URL{Scheme: "sqlite", Path: filepath.Join(t.TempDir(), "todo.db")}).String()
	st, err := store.Open(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := clocktest.New(testNow)
	svc := service.New(st, clk, service.Config{DefaultTZ: time.UTC, DefaultHour: 9})
	h := &harness{t: t, st: st, svc: svc, clk: clk, send: &tgtest.FakeSender{}, members: &tgtest.FakeMembers{}, log: &syncBuf{}}
	h.srv = New(Deps{
		Svc: svc, Web: svc, Send: h.send, Members: h.members,
		Parser: nlparse.Parser{DefaultHour: 9}, Clock: clk,
		Allowed: map[int64]bool{idAnn: true, idBob: true},
		Token:   testToken, BotName: "TodoBot", Config: testConfig(), Assets: testAssets,
		Log: slog.New(slog.NewTextHandler(h.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	return h
}

type reqOpt func(*http.Request)

func bearer(tok string) reqOpt {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func cookie(tok string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookieName, Value: tok}) }
}

func header(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }

func delHeader(k string) reqOpt { return func(r *http.Request) { r.Header.Del(k) } }

func peer(addr string) reqOpt { return func(r *http.Request) { r.RemoteAddr = addr } }

func host(hst string) reqOpt { return func(r *http.Request) { r.Host = hst } }

// do sends a request straight to the handler. An unsafe method gets the
// CSRF headers of the app, and a body gets Content-Type: application/json;
// options run after that, so they can remove them.
func (h *harness) do(method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	h.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Host = testHost
	r.RemoteAddr = "127.0.0.1:40000"
	if unsafeMethod(method) {
		r.Header.Set("X-Todo-Request", "1")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("Origin", testOrigin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	h.srv.ServeHTTP(w, r)
	return w
}

// initData signs fields of user, made now.
func (h *harness) initData(user string) string {
	return signFields(testToken, fields(h.clk.Now(), user))
}

func signInBody(initData string) string {
	b, _ := json.Marshal(map[string]string{"init_data": initData})
	return string(b)
}

// signIn signs user in through endpoint 4 and returns the token.
func (h *harness) signIn(user string) string {
	h.t.Helper()
	w := h.do("POST", "/api/auth/miniapp", signInBody(h.initData(user)), peer(nextIP()))
	if w.Code != http.StatusOK {
		h.t.Fatalf("sign-in: %d %s", w.Code, w.Body)
	}
	var resp miniAppResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		h.t.Fatal(err)
	}
	return resp.Token
}

// browser starts a browser session directly in the service: W3 has no
// HTTP way to make one.
func (h *harness) browser(user int64) (string, model.WebSession) {
	h.t.Helper()
	cfg := testConfig()
	tok, ws, err := h.svc.StartWebSession(context.Background(), user, model.WebBrowser, "UA", cfg.WebSessionTTL, cfg.WebSessionMax)
	if err != nil {
		h.t.Fatal(err)
	}
	return tok, ws
}

var (
	ipMu sync.Mutex
	ipN  int
)

// nextIP gives a new peer address (not a trusted proxy) each time, so that
// sign-ins in a test do not hit the limit per IP. Tests of that limit use
// a fixed peer.
func nextIP() string {
	ipMu.Lock()
	defer ipMu.Unlock()
	ipN++
	return "10.1." + strconv.Itoa(ipN/250) + "." + strconv.Itoa(ipN%250+1) + ":1234"
}

type errBody struct {
	Error map[string]any `json:"error"`
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var b errBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("not a JSON error body (%d): %q", w.Code, w.Body)
	}
	c, _ := b.Error["code"].(string)
	if m, _ := b.Error["message"].(string); m == "" || strings.HasPrefix(m, "web.err.") {
		t.Errorf("error %q has no message: %q", c, m)
	}
	return c
}

func wantErr(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d; body %s", w.Code, status, w.Body)
	}
	if got := errCode(t, w); got != code {
		t.Fatalf("code %q, want %q", got, code)
	}
}

func TestHealthz(t *testing.T) {
	h := newHarness(t)
	_ = h.st.Close() // liveness does not touch the database
	w := h.do("GET", "/healthz", "", host("127.0.0.1:8088"))
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("healthz: %d %q", w.Code, w.Body)
	}
}

func TestReadyz(t *testing.T) {
	h := newHarness(t)
	w := h.do("GET", "/readyz", "", host("127.0.0.1:8088"))
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("readyz: %d %q", w.Code, w.Body)
	}
	_ = h.st.Close()
	if w := h.do("GET", "/readyz", "", peer("127.0.0.2:1")); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with a closed database: %d", w.Code)
	}
}

func TestReadyzRateLimit(t *testing.T) {
	h := newHarness(t)
	for i := range readyzPerMin {
		if w := h.do("GET", "/readyz", "", peer("10.0.0.1:1")); w.Code != 200 {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	w := h.do("GET", "/readyz", "", peer("10.0.0.1:1"))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("over the limit: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	if w := h.do("GET", "/readyz", "", peer("10.0.0.2:1")); w.Code != 200 {
		t.Fatalf("another IP: %d", w.Code)
	}
	h.clk.Advance(time.Minute)
	if w := h.do("GET", "/readyz", "", peer("10.0.0.1:1")); w.Code != 200 {
		t.Fatalf("after a minute: %d", w.Code)
	}
}

func TestHostCheck(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/", "/tg/", "/api/me", "/api/i18n?lang=en", "/sw.js"} {
		w := h.do("GET", p, "", host("127.0.0.1:8088"))
		wantErr(t, w, http.StatusMisdirectedRequest, "misdirected")
	}
	if w := h.do("POST", "/api/auth/miniapp", "{}", host("evil.example")); w.Code != http.StatusMisdirectedRequest {
		t.Errorf("POST with another host: %d", w.Code)
	}
	if w := h.do("GET", "/", "", host("TODO.Example.com")); w.Code != 200 {
		t.Errorf("host in another case: %d", w.Code)
	}
	for _, p := range []string{"/healthz", "/readyz"} {
		if w := h.do("GET", p, "", host("127.0.0.1:8088")); w.Code != 200 {
			t.Errorf("%s with the loopback host: %d", p, w.Code)
		}
	}
}

func TestCommonHeaders(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	for _, w := range []*httptest.ResponseRecorder{
		h.do("GET", "/", ""),
		h.do("GET", "/tg/", ""),
		h.do("GET", "/assets/app-1a2b3c.js", ""),
		h.do("GET", "/api/me", "", bearer(tok)),
		h.do("GET", "/api/me", ""),                    // 401
		h.do("GET", "/nothing", ""),                   // 404
		h.do("GET", "/healthz", ""),                   // health
		h.do("GET", "/api/me", "", host("x.example")), // 421
	} {
		hd := w.Header()
		for k, v := range map[string]string{
			"X-Content-Type-Options":    "nosniff",
			"Referrer-Policy":           "no-referrer",
			"Permissions-Policy":        "camera=(), microphone=(), geolocation=(), payment=()",
			"Strict-Transport-Security": "max-age=31536000",
		} {
			if hd.Get(k) != v {
				t.Errorf("%d response: %s = %q, want %q", w.Code, k, hd.Get(k), v)
			}
		}
		if hd.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("a CORS header was sent")
		}
	}
}

func TestAPIHeaders(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	for _, w := range []*httptest.ResponseRecorder{
		h.do("GET", "/api/me", "", bearer(tok)),
		h.do("GET", "/api/me", ""),
		h.do("POST", "/api/auth/miniapp", "{}"),
		h.do("GET", "/api/nothing", "", bearer(tok)),
	} {
		if got := w.Header().Get("Content-Security-Policy"); got != cspAPI {
			t.Errorf("%d: CSP %q", w.Code, got)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%d: Cache-Control %q", w.Code, got)
		}
		if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("%d: Content-Type %q", w.Code, got)
		}
	}
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	wantErr(t, h.do("GET", "/api/nothing", "", bearer(tok)), 404, "not_found")
	wantErr(t, h.do("GET", "/api/auth/miniapp", ""), 404, "not_found") // a wrong method
}

func TestCSRFGuard(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	btok, _ := h.browser(idBob)
	unsafe := []struct{ method, path string }{
		{"POST", "/api/auth/miniapp"}, // 4, no session
		{"POST", "/api/auth/logout"},
		{"POST", "/api/auth/logout-all"},
		{"POST", "/api/auth/code"},
		{"POST", "/api/auth/code/redeem"}, // 13, no session
		{"DELETE", "/api/auth/code"},
		{"POST", "/api/parse"},
		{"POST", "/api/chats/42/items"},
		{"POST", "/api/chats/42/items/1/done"},
		{"DELETE", "/api/chats/42/items/1"},
		{"POST", "/api/chats/42/items/1/remind"},
		{"PUT", "/api/me/settings"},
		{"PATCH", "/api/chats/42/items/1"},
		{"POST", "/api/nothing"},
	}
	bad := []struct {
		name string
		opts []reqOpt
	}{
		{"no X-Todo-Request", []reqOpt{delHeader("X-Todo-Request")}},
		{"wrong X-Todo-Request", []reqOpt{header("X-Todo-Request", "yes")}},
		{"cross-site", []reqOpt{header("Sec-Fetch-Site", "cross-site")}},
		{"same-site", []reqOpt{header("Sec-Fetch-Site", "same-site")}},
		{"other origin", []reqOpt{header("Origin", "https://evil.example")}},
		{"http origin", []reqOpt{header("Origin", "http://todo.example.com")}},
		{"no origin, no fetch site", []reqOpt{delHeader("Origin"), delHeader("Sec-Fetch-Site")}},
		{"text body", []reqOpt{header("Content-Type", "text/plain")}},
		{"form body", []reqOpt{header("Content-Type", "application/x-www-form-urlencoded")}},
	}
	for _, rt := range unsafe {
		for _, b := range bad {
			for _, auth := range []struct {
				name string
				opt  reqOpt
			}{{"none", func(*http.Request) {}}, {"bearer", bearer(tok)}, {"cookie", cookie(btok)}} {
				opts := append([]reqOpt{auth.opt}, b.opts...)
				w := h.do(rt.method, rt.path, `{}`, opts...)
				if w.Code != http.StatusForbidden || errCode(t, w) != "csrf" {
					t.Errorf("%s %s, %s, %s: %d %s", rt.method, rt.path, b.name, auth.name, w.Code, w.Body)
				}
			}
		}
	}
	// One of Origin and Sec-Fetch-Site is enough, and a request with no
	// body needs no Content-Type.
	for _, opts := range [][]reqOpt{
		{delHeader("Origin")},
		{delHeader("Sec-Fetch-Site")},
	} {
		if w := h.do("POST", "/api/auth/miniapp", signInBody(h.initData(userAnn)), append(opts, peer(nextIP()))...); w.Code != 200 {
			t.Errorf("a correct request was refused: %d %s", w.Code, w.Body)
		}
	}
	if w := h.do("POST", "/api/auth/logout", "", bearer(tok), delHeader("Content-Type")); w.Code != http.StatusNoContent {
		t.Errorf("logout with no body: %d %s", w.Code, w.Body)
	}
}

func TestBodyLimit(t *testing.T) {
	h := newHarness(t)
	big := `{"init_data":"` + strings.Repeat("a", maxBody) + `"}`
	wantErr(t, h.do("POST", "/api/auth/miniapp", big), http.StatusRequestEntityTooLarge, "too_large")
}

func TestBusy(t *testing.T) {
	h := newHarness(t)
	for range maxInFlight {
		h.srv.sem <- struct{}{}
	}
	wantErr(t, h.do("GET", "/api/i18n?lang=en", ""), http.StatusServiceUnavailable, "busy")
	if w := h.do("GET", "/healthz", ""); w.Code != 200 {
		t.Errorf("healthz while busy: %d", w.Code)
	}
	<-h.srv.sem
	if w := h.do("GET", "/api/i18n?lang=en", ""); w.Code != 200 {
		t.Errorf("after one request ended: %d", w.Code)
	}
}

func TestPanicGives500(t *testing.T) {
	h := newHarness(t)
	h.srv.mux.HandleFunc("GET /api/test-panic", func(http.ResponseWriter, *http.Request) { panic("secret-value") })
	wantErr(t, h.do("GET", "/api/test-panic", ""), 500, "internal")
	if strings.Contains(h.log.String(), "secret-value") {
		t.Error("the panic value is in the log")
	}
}

// TestServeAndShutdown runs the server on a real listener.
func TestServeAndShutdown(t *testing.T) {
	h := newHarness(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.srv.Serve(l) }()
	resp, err := http.Get("http://" + l.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "ok" {
		t.Fatalf("healthz: %d %q", resp.StatusCode, b)
	}
	if h.srv.hs.ReadHeaderTimeout != 5*time.Second || h.srv.hs.ReadTimeout != 10*time.Second ||
		h.srv.hs.WriteTimeout != 15*time.Second || h.srv.hs.IdleTimeout != 60*time.Second || h.srv.hs.MaxHeaderBytes != 8<<10 {
		t.Errorf("server limits: %+v", h.srv.hs)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve after Shutdown: %v", err)
	}
	if resp, err := http.Get("http://" + l.Addr().String() + "/healthz"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("the listener still answers")
	}
}

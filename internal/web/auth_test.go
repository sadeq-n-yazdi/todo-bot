package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

var tokenRE = regexp.MustCompile(`^tb1_[A-Za-z0-9_-]{43}$`)

func TestMiniAppSignIn(t *testing.T) {
	h := newHarness(t)
	w := h.do("POST", "/api/auth/miniapp", signInBody(h.initData(userAnn)), header("User-Agent", "TelegramWebView"))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var resp miniAppResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !tokenRE.MatchString(resp.Token) {
		t.Errorf("token %q", resp.Token)
	}
	if want := testNow.Add(12 * time.Hour); !resp.ExpiresAt.Equal(want) {
		t.Errorf("expires_at %v, want %v", resp.ExpiresAt, want)
	}
	if resp.User.ID != idAnn || resp.Start != "g-100" {
		t.Errorf("user %d, start %q", resp.User.ID, resp.Start)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("a Mini App sign-in set a cookie")
	}
	ws, err := h.svc.WebSession(context.Background(), resp.Token, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != model.WebMiniApp || ws.UserID != idAnn || !ws.MaxAt.Equal(ws.ExpiresAt) || ws.Device != "TelegramWebView" {
		t.Errorf("session %+v", ws)
	}
	// Prefs created the user with the Telegram language; NoteUsername
	// stored the name.
	u, err := h.svc.User(context.Background(), idAnn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Lang != i18n.Fa || u.Username != "ann" {
		t.Errorf("user %+v", u)
	}
}

func TestMiniAppStartParam(t *testing.T) {
	h := newHarness(t)
	for param, want := range map[string]string{
		"code": "code", "g-100": "g-100", "g100": "g100", "": "",
		"g": "", "code2": "", "<script>": "", "g-1x": "", "g-12345678901234567890": "",
	} {
		f := fields(h.clk.Now(), userAnn)
		if param == "" {
			delete(f, "start_param")
		} else {
			f["start_param"] = param
		}
		w := h.do("POST", "/api/auth/miniapp", signInBody(signFields(testToken, f)), peer(nextIP()))
		var resp miniAppResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if w.Code != 200 || resp.Start != want {
			t.Errorf("start_param %q: %d, start %q, want %q", param, w.Code, resp.Start, want)
		}
	}
}

func TestMiniAppSignInErrors(t *testing.T) {
	h := newHarness(t)
	now := h.clk.Now()
	badSig := signFields("999:OTHER", fields(now, userAnn))
	f := fields(now, userAnn)
	f["signature"] = "abc"
	withSignature := signFields(testToken, f)
	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"not JSON", `nope`, 400, "bad_request"},
		{"empty body", ``, 400, "bad_request"},
		{"unknown field", `{"init_data":"x","more":1}`, 400, "bad_request"},
		{"two values", `{"init_data":"x"}{}`, 400, "bad_request"},
		{"empty init_data", `{"init_data":""}`, 400, "bad_request"},
		{"init_data above 4 KiB", signInBody(strings.Repeat("a", 4<<10+1)), 400, "bad_request"},
		{"duplicate key", signInBody(h.initData(userAnn) + "&auth_date=1"), 400, "bad_request"},
		{"wrong hash", signInBody(badSig), 401, "bad_signature"},
		{"no hash", signInBody(strings.Split(h.initData(userAnn), "&hash=")[0]), 401, "bad_signature"},
		{"too old", signInBody(signFields(testToken, fields(now.Add(-time.Hour-time.Second), userAnn))), 401, "expired"},
		{"future", signInBody(signFields(testToken, fields(now.Add(61*time.Second), userAnn))), 401, "expired"},
		{"user id 0", signInBody(signFields(testToken, fields(now, `{"id":0}`))), 400, "bad_request"},
		{"signature field", signInBody(withSignature), 200, ""},
	}
	for _, c := range cases {
		opts := []reqOpt{peer(nextIP())}
		if c.body == "" {
			opts = append(opts, delHeader("Content-Type"))
		}
		w := h.do("POST", "/api/auth/miniapp", c.body, opts...)
		if c.status == 200 {
			if w.Code != 200 {
				t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
			}
			continue
		}
		if w.Code != c.status || errCode(t, w) != c.code {
			t.Errorf("%s: %d %s, want %d %s", c.name, w.Code, w.Body, c.status, c.code)
		}
	}
}

func TestMiniAppNotAllowed(t *testing.T) {
	h := newHarness(t)
	w := h.do("POST", "/api/auth/miniapp", signInBody(h.initData(userEve)))
	wantErr(t, w, 403, "not_allowed")
	var b errBody
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	if b.Error["user_id"] != float64(idEve) {
		t.Errorf("user_id %v", b.Error["user_id"])
	}
	if _, err := h.svc.User(context.Background(), idEve); err == nil {
		t.Error("a user who is not allowed was created")
	}
}

func TestMiniAppRateLimit(t *testing.T) {
	h := newHarness(t)
	for i := range miniAppPerMin {
		if w := h.do("POST", "/api/auth/miniapp", signInBody(h.initData(userAnn)), peer("10.9.9.9:1")); w.Code != 200 {
			t.Fatalf("sign-in %d: %d", i+1, w.Code)
		}
	}
	w := h.do("POST", "/api/auth/miniapp", signInBody(h.initData(userAnn)), peer("10.9.9.9:1"))
	wantErr(t, w, 429, "rate_limited")
	if w.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
	// Behind the trusted proxy, the limit is per CF-Connecting-IP.
	for i := range miniAppPerMin {
		w := h.do("POST", "/api/auth/miniapp", signInBody(h.initData(userAnn)), header("CF-Connecting-IP", "203.0.113."+strconv.Itoa(i+1)))
		if w.Code != 200 {
			t.Fatalf("client %d behind the proxy: %d", i+1, w.Code)
		}
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	other := h.signIn(userAnn)
	w := h.do("POST", "/api/auth/logout", "", bearer(tok))
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", w.Code, w.Body)
	}
	if c := w.Result().Cookies(); len(c) != 1 || c[0].Name != cookieName || c[0].MaxAge != -1 || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("cookie not cleared: %q", w.Header().Get("Set-Cookie"))
	}
	wantErr(t, h.do("GET", "/api/me", "", bearer(tok)), 401, "unauthenticated")
	if w := h.do("GET", "/api/me", "", bearer(other)); w.Code != 200 {
		t.Errorf("the other session of the user: %d", w.Code)
	}
	wantErr(t, h.do("POST", "/api/auth/logout", "", bearer(tok)), 401, "unauthenticated")
	wantErr(t, h.do("POST", "/api/auth/logout", ""), 401, "unauthenticated")
}

func TestLogoutAll(t *testing.T) {
	h := newHarness(t)
	mini := h.signIn(userAnn)
	b1, _ := h.browser(idAnn)
	b2, _ := h.browser(idAnn)
	bob := h.signIn(userBob)
	w := h.do("POST", "/api/auth/logout-all", "", cookie(b1))
	if w.Code != http.StatusNoContent || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("logout-all: %d %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	for _, tok := range []string{mini, b2} {
		wantErr(t, h.do("GET", "/api/me", "", bearer(tok)), 401, "unauthenticated")
	}
	if w := h.do("GET", "/api/me", "", bearer(bob)); w.Code != 200 {
		t.Errorf("another user lost the session: %d", w.Code)
	}
}

// TestNoSecretsInLog captures the log of a full sign-in: the Mini App
// sign-in, requests with the token and with a cookie, and the logout.
func TestNoSecretsInLog(t *testing.T) {
	h := newHarness(t)
	raw := h.initData(userAnn)
	q, _ := url.ParseQuery(raw)
	w := h.do("POST", "/api/auth/miniapp", signInBody(raw))
	var resp miniAppResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	h.do("GET", "/api/me", "", bearer(resp.Token))
	h.do("GET", "/api/chats", "", bearer(resp.Token))
	h.do("POST", "/api/auth/code", `{"init_data":"`+raw+`"}`, bearer(resp.Token))
	btok, _ := h.browser(idAnn)
	h.clk.Advance(2 * time.Hour) // the cookie goes out again
	h.do("GET", "/api/me", "", cookie(btok))
	h.do("GET", "/auth/telegram?id=42&hash=deadbeefcafe&auth_date=1", "")
	h.do("POST", "/api/auth/logout", "", bearer(resp.Token))
	h.do("POST", "/api/auth/logout", "", bearer("tb1_"+strings.Repeat("Z", 43)))

	log := h.log.String()
	if !strings.Contains(log, "web request") || !strings.Contains(log, "route=\"POST /api/auth/miniapp\"") || !strings.Contains(log, "user=42") {
		t.Fatalf("the log lacks the request lines: %s", log)
	}
	secrets := map[string]string{
		"token": resp.Token, "browser token": btok, "token prefix": "tb1_",
		"hash": q.Get("hash"), "init data": raw, "auth_date": q.Get("auth_date"),
		"query id": q.Get("query_id"), "widget hash": "deadbeefcafe", "bot token": testToken,
		"cookie": cookieName, "username": "ann",
	}
	for name, v := range secrets {
		if v != "" && strings.Contains(log, v) {
			t.Errorf("the log holds the %s", name)
		}
	}
}

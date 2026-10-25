package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

func meUserID(t *testing.T, w *httptest.ResponseRecorder) int64 {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("/api/me: %d %s", w.Code, w.Body)
	}
	var me meResponse
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	return me.User.ID
}

func TestSessionTransport(t *testing.T) {
	h := newHarness(t)
	ann := h.signIn(userAnn)
	bob, _ := h.browser(idBob)
	if id := meUserID(t, h.do("GET", "/api/me", "", bearer(ann))); id != idAnn {
		t.Errorf("bearer: user %d", id)
	}
	if id := meUserID(t, h.do("GET", "/api/me", "", cookie(bob))); id != idBob {
		t.Errorf("cookie: user %d", id)
	}
	if id := meUserID(t, h.do("GET", "/api/me", "", bearer(ann), cookie(bob))); id != idAnn {
		t.Errorf("both: user %d, want the bearer user", id)
	}
	// A bad header also wins: the cookie is not used.
	wantErr(t, h.do("GET", "/api/me", "", bearer("junk"), cookie(bob)), 401, "unauthenticated")
	wantErr(t, h.do("GET", "/api/me", "", header("Authorization", "Basic abc"), cookie(bob)), 401, "unauthenticated")
}

func TestSessionBadTokens(t *testing.T) {
	h := newHarness(t)
	ann := h.signIn(userAnn)
	for _, tok := range []string{"", "x", "tb1_short", "tb2_" + ann[4:], ann + "A", ann[:len(ann)-1] + "!", "tb1_" + strings.Repeat("A", 43)} {
		wantErr(t, h.do("GET", "/api/me", "", bearer(tok)), 401, "unauthenticated")
	}
	wantErr(t, h.do("GET", "/api/me", ""), 401, "unauthenticated")
	// An unknown cookie is cleared.
	w := h.do("GET", "/api/me", "", cookie("tb1_"+strings.Repeat("A", 43)))
	wantErr(t, w, 401, "unauthenticated")
	if !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("unknown cookie not cleared: %q", w.Header().Get("Set-Cookie"))
	}
}

func TestMiniAppSessionExpiry(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	h.clk.Advance(12*time.Hour - time.Second)
	if w := h.do("GET", "/api/me", "", bearer(tok)); w.Code != 200 {
		t.Fatalf("before 12 h: %d", w.Code)
	}
	h.clk.Advance(time.Second)
	wantErr(t, h.do("GET", "/api/me", "", bearer(tok)), 401, "unauthenticated")
}

func cookieMaxAge(t *testing.T, w *httptest.ResponseRecorder) (int, bool) {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieName {
			if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" {
				t.Errorf("cookie attributes: %+v", c)
			}
			return c.MaxAge, true
		}
	}
	return 0, false
}

func TestBrowserSessionSlides(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.browser(idAnn)
	h.clk.Advance(30 * time.Minute)
	w := h.do("GET", "/api/me", "", cookie(tok))
	if _, ok := cookieMaxAge(t, w); ok || w.Code != 200 {
		t.Fatalf("within the hour: %d, a cookie was sent", w.Code)
	}
	h.clk.Advance(30 * time.Minute)
	w = h.do("GET", "/api/me", "", cookie(tok))
	age, ok := cookieMaxAge(t, w)
	if !ok || age != int((720*time.Hour)/time.Second) {
		t.Fatalf("after one hour: cookie %v, Max-Age %d", ok, age)
	}
	ws, err := h.svc.WebSession(context.Background(), tok, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := testNow.Add(time.Hour + 720*time.Hour); !ws.ExpiresAt.Equal(want) {
		t.Errorf("expires_at %v, want %v", ws.ExpiresAt, want)
	}
	// The next request in the same hour writes nothing and sends no cookie.
	h.clk.Advance(10 * time.Minute)
	if _, ok := cookieMaxAge(t, h.do("GET", "/api/me", "", cookie(tok))); ok {
		t.Error("a second cookie in the same hour")
	}
	// A bearer request slides too, but sends no cookie.
	h.clk.Advance(2 * time.Hour)
	if _, ok := cookieMaxAge(t, h.do("GET", "/api/me", "", bearer(tok))); ok {
		t.Error("a cookie for a bearer request")
	}
}

func TestBrowserSessionIdleAndAbsoluteLimit(t *testing.T) {
	h := newHarness(t)
	idle, _ := h.browser(idAnn)
	h.clk.Advance(720 * time.Hour)
	wantErr(t, h.do("GET", "/api/me", "", cookie(idle)), 401, "unauthenticated")

	h = newHarness(t)
	tok, _ := h.browser(idAnn)
	for range 3 { // 700 h, 1400 h, 2100 h
		h.clk.Advance(700 * time.Hour)
		if w := h.do("GET", "/api/me", "", cookie(tok)); w.Code != 200 {
			t.Fatalf("at %v: %d", h.clk.Now().Sub(testNow), w.Code)
		}
	}
	h.clk.Advance(10 * time.Hour)
	w := h.do("GET", "/api/me", "", cookie(tok))
	if age, ok := cookieMaxAge(t, w); !ok || age != int((50*time.Hour)/time.Second) {
		t.Fatalf("near the absolute limit: Max-Age %d, want 50 h", age)
	}
	h.clk.Advance(50 * time.Hour) // 2160 h
	wantErr(t, h.do("GET", "/api/me", "", cookie(tok)), 401, "unauthenticated")
}

func TestSessionRevokedAllowlist(t *testing.T) {
	h := newHarness(t)
	eve1, _ := h.browser(idEve)
	eve2, _ := h.browser(idEve)
	ann := h.signIn(userAnn)
	w := h.do("GET", "/api/me", "", cookie(eve1))
	wantErr(t, w, 401, "session_revoked")
	if !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Error("the cookie was not cleared")
	}
	// Every session of the user is gone.
	wantErr(t, h.do("GET", "/api/me", "", cookie(eve2)), 401, "unauthenticated")
	if w := h.do("GET", "/api/me", "", bearer(ann)); w.Code != 200 {
		t.Errorf("an allowed user: %d", w.Code)
	}
}

func live(h *harness, tok string) bool {
	return h.do("GET", "/api/me", "", bearer(tok)).Code == 200
}

func TestSessionLimitsPerKind(t *testing.T) {
	h := newHarness(t)
	var browsers []string
	for range 20 {
		tok, _ := h.browser(idAnn)
		browsers = append(browsers, tok)
		h.clk.Advance(time.Second)
	}
	var minis []string
	for range 6 {
		minis = append(minis, h.signIn(userAnn))
		h.clk.Advance(time.Second)
	}
	if live(h, minis[0]) {
		t.Error("the oldest of 6 miniapp sessions is still live")
	}
	for i, tok := range minis[1:] {
		if !live(h, tok) {
			t.Errorf("miniapp session %d is not live", i+1)
		}
	}
	for i, tok := range browsers {
		if !live(h, tok) {
			t.Errorf("browser session %d was removed by miniapp sign-ins", i)
		}
	}
	extra, _ := h.browser(idAnn)
	if live(h, browsers[0]) || !live(h, browsers[1]) || !live(h, extra) {
		t.Error("the 21st browser session did not remove only the oldest")
	}
	for _, tok := range minis[1:] {
		if !live(h, tok) {
			t.Error("a browser session removed a miniapp session")
		}
	}
}

func TestMiniAppOnlyRoutes(t *testing.T) {
	h := newHarness(t)
	btok, _ := h.browser(idAnn)
	for _, rt := range []struct{ method, path string }{{"POST", "/api/auth/code"}, {"DELETE", "/api/auth/code"}} {
		wantErr(t, h.do(rt.method, rt.path, `{}`, cookie(btok)), 403, "wrong_session")
		wantErr(t, h.do(rt.method, rt.path, `{}`), 401, "unauthenticated")
	}
	mini := h.signIn(userAnn)
	wantErr(t, h.do("POST", "/api/auth/code", `{}`, bearer(mini)), 401, "bad_signature")
}

func TestUserRateLimit(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	other := h.signIn(userBob)
	for i := range userPerMin {
		if w := h.do("GET", "/api/me", "", bearer(tok)); w.Code != 200 {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	w := h.do("GET", "/api/chats", "", bearer(tok))
	wantErr(t, w, 429, "rate_limited")
	if ra, err := strconv.Atoi(w.Header().Get("Retry-After")); err != nil || ra < 1 {
		t.Errorf("Retry-After %q", w.Header().Get("Retry-After"))
	}
	if w := h.do("GET", "/api/me", "", bearer(other)); w.Code != 200 {
		t.Errorf("another user: %d", w.Code)
	}
	h.clk.Advance(time.Second)
	if w := h.do("GET", "/api/me", "", bearer(tok)); w.Code != 200 {
		t.Errorf("after one token came back: %d", w.Code)
	}
}

// The login code route has its own limit of 5 in 10 minutes per user.
func TestCodeRouteLimit(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	for range codePer10Min {
		wantErr(t, h.do("POST", "/api/auth/code", `{}`, bearer(tok)), 401, "bad_signature")
	}
	wantErr(t, h.do("POST", "/api/auth/code", `{}`, bearer(tok)), 429, "rate_limited")
	if w := h.do("GET", "/api/me", "", bearer(tok)); w.Code != 200 {
		t.Errorf("the code limit stopped another route: %d", w.Code)
	}
}

func TestSessionKindInMe(t *testing.T) {
	h := newHarness(t)
	btok, ws := h.browser(idBob)
	var me meResponse
	_ = json.Unmarshal(h.do("GET", "/api/me", "", cookie(btok)).Body.Bytes(), &me)
	if me.Session.Kind != string(model.WebBrowser) || !me.Session.ExpiresAt.Equal(ws.ExpiresAt) {
		t.Errorf("session %+v", me.Session)
	}
}

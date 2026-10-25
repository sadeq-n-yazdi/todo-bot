package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

var codeRE = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)

// newCode makes a code for the user of the Mini App token through endpoint
// 12 and returns it as shown (ABCD-EFGH).
func (h *harness) newCode(token, user string) string {
	h.t.Helper()
	w := h.do("POST", "/api/auth/code", signInBody(h.initData(user)), bearer(token))
	if w.Code != http.StatusCreated {
		h.t.Fatalf("new code: %d %s", w.Code, w.Body)
	}
	var resp codeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		h.t.Fatal(err)
	}
	return resp.Code
}

func codeBody(code string) string {
	b, _ := json.Marshal(map[string]string{"code": code})
	return string(b)
}

func (h *harness) redeem(code string, opts ...reqOpt) *httptest.ResponseRecorder {
	h.t.Helper()
	defer h.srv.codes.notices.Wait() // the notice goes out in the background
	return h.do("POST", "/api/auth/code/redeem", codeBody(code), append([]reqOpt{peer(nextIP())}, opts...)...)
}

func TestNewCode(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	w := h.do("POST", "/api/auth/code", signInBody(h.initData(userAnn)), bearer(tok))
	if w.Code != http.StatusCreated {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var resp codeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !codeRE.MatchString(resp.Code) {
		t.Errorf("code %q", resp.Code)
	}
	if want := h.clk.Now().Add(5 * time.Minute); !resp.ExpiresAt.Equal(want) {
		t.Errorf("expires_at %v, want %v", resp.ExpiresAt, want)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q", cc)
	}
	if other := h.newCode(tok, userAnn); other == resp.Code {
		t.Error("two codes are equal")
	}
}

func TestNewCodeNeedsFreshProofOfTheSameUser(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	n := 0
	// post sends a request. The limit is 5 in 10 minutes for each user, so
	// the clock moves on after every 4 requests.
	post := func(body func(now time.Time) string, opts ...reqOpt) *httptest.ResponseRecorder {
		if n++; n%4 == 0 {
			h.clk.Advance(10 * time.Minute)
		}
		return h.do("POST", "/api/auth/code", body(h.clk.Now()), append([]reqOpt{bearer(tok)}, opts...)...)
	}
	lit := func(s string) func(time.Time) string { return func(time.Time) string { return s } }
	aged := func(user string, age time.Duration) func(time.Time) string {
		return func(now time.Time) string { return signInBody(signFields(testToken, fields(now.Add(-age), user))) }
	}
	// The Mini App token alone gives 401.
	wantErr(t, post(lit("")), 401, "bad_signature")
	wantErr(t, post(lit(`{}`)), 401, "bad_signature")
	wantErr(t, post(lit(`{"init_data":""}`)), 401, "bad_signature")
	// Bad shape and bad proof.
	wantErr(t, post(lit(`{"init_data":"x","more":1}`)), 400, "bad_request")
	wantErr(t, post(lit(signInBody("hash=00&auth_date=1"))), 401, "bad_signature")
	wantErr(t, post(func(now time.Time) string { return signInBody(signFields("1:OTHER", fields(now, userAnn))) }), 401, "bad_signature")
	// 10 minutes is the limit, not 1 hour.
	wantErr(t, post(aged(userAnn, 10*time.Minute+time.Second)), 401, "expired")
	if w := post(aged(userAnn, 10*time.Minute)); w.Code != 201 {
		t.Errorf("proof of 10 min: %d %s", w.Code, w.Body)
	}
	// Fresh initData of another user.
	wantErr(t, post(aged(userBob, 0)), 403, "wrong_user")
	// No session, and a browser session.
	wantErr(t, h.do("POST", "/api/auth/code", signInBody(h.initData(userAnn))), 401, "unauthenticated")
	btok, _ := h.browser(idAnn)
	wantErr(t, h.do("POST", "/api/auth/code", signInBody(h.initData(userAnn)), cookie(btok)), 403, "wrong_session")
}

func TestNewCodeRateLimit(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	for i := range codePer10Min {
		if w := h.do("POST", "/api/auth/code", signInBody(h.initData(userAnn)), bearer(tok)); w.Code != 201 {
			t.Fatalf("code %d: %d", i+1, w.Code)
		}
	}
	w := h.do("POST", "/api/auth/code", signInBody(h.initData(userAnn)), bearer(tok))
	wantErr(t, w, 429, "rate_limited")
	if w.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
}

func TestRedeemSignsIn(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	code := h.newCode(tok, userAnn)
	// The user may type it in lower case, with a space, with no dash.
	typed := strings.ToLower(strings.Replace(code, "-", " ", 1))
	w := h.redeem(typed, header("User-Agent", "Safari"), peer("203.0.113.9:5555"))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var resp redeemResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	cs := w.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != cookieName || !tokenRE.MatchString(cs[0].Value) ||
		!cs[0].Secure || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteStrictMode || cs[0].Path != "/" {
		t.Fatalf("cookie %+v", cs)
	}
	if resp.User.ID != idAnn || !resp.ExpiresAt.Equal(h.clk.Now().Add(720*time.Hour)) {
		t.Errorf("response %+v", resp)
	}
	if strings.Contains(w.Body.String(), cs[0].Value) {
		t.Error("the body holds the token")
	}
	ws, err := h.svc.WebSession(context.Background(), cs[0].Value, time.Hour)
	if err != nil || ws.Kind != model.WebBrowser || ws.UserID != idAnn || ws.Device != "Safari" {
		t.Fatalf("session %+v, %v", ws, err)
	}
	// The PWA has the cookie now, and the tabs open.
	me := h.do("GET", "/api/me", "", cookie(cs[0].Value))
	if me.Code != 200 {
		t.Errorf("/api/me with the cookie: %d", me.Code)
	}
}

func TestRedeemFailures(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	code := h.newCode(tok, userAnn)
	wantErr(t, h.redeem("ZZZZ-ZZZZ"), 401, "bad_code")
	wantErr(t, h.redeem("short"), 401, "bad_code")
	wantErr(t, h.redeem(strings.Repeat("A", 5000)), 401, "bad_code")
	if w := h.redeem(code); w.Code != 200 {
		t.Fatalf("first redeem: %d %s", w.Code, w.Body)
	}
	wantErr(t, h.redeem(code), 401, "bad_code") // used

	expired := h.newCode(tok, userAnn)
	h.clk.Advance(5*time.Minute + time.Second)
	wantErr(t, h.redeem(expired), 401, "bad_code") // expired
	if len(h.send.Messages()) != 1 {
		t.Errorf("%d notices, want 1", len(h.send.Messages()))
	}

	// Bad requests.
	wantErr(t, h.redeem(""), 400, "bad_request")
	wantErr(t, h.redeem("  "), 400, "bad_request")
	wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":"A","x":1}`, peer(nextIP())), 400, "bad_request")
	wantErr(t, h.do("POST", "/api/auth/code/redeem", ``, peer(nextIP()), delHeader("Content-Type")), 400, "bad_request")
	wantErr(t, h.do("POST", "/api/auth/code/redeem", codeBody("A"), peer(nextIP()), delHeader("X-Todo-Request")), 403, "csrf")
}

func TestOneLiveCodePerUser(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	first := h.newCode(tok, userAnn)
	second := h.newCode(tok, userAnn)
	wantErr(t, h.redeem(first), 401, "bad_code")
	if w := h.redeem(second); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// Other users keep their codes.
	bob := h.signIn(userBob)
	a, b := h.newCode(h.signIn(userAnn), userAnn), h.newCode(bob, userBob)
	if w := h.redeem(a); w.Code != 200 {
		t.Errorf("ann: %d", w.Code)
	}
	if w := h.redeem(b); w.Code != 200 {
		t.Errorf("bob: %d", w.Code)
	}
}

func TestCancelCode(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	bobTok := h.signIn(userBob)
	code := h.newCode(tok, userAnn)
	bobCode := h.newCode(bobTok, userBob)
	if w := h.do("DELETE", "/api/auth/code", "", bearer(tok)); w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	wantErr(t, h.redeem(code), 401, "bad_code")
	if w := h.redeem(bobCode); w.Code != 200 {
		t.Errorf("the code of another user: %d", w.Code)
	}
	// With no code it still answers 204.
	if w := h.do("DELETE", "/api/auth/code", "", bearer(tok)); w.Code != http.StatusNoContent {
		t.Errorf("second cancel: %d", w.Code)
	}
	wantErr(t, h.do("DELETE", "/api/auth/code", ""), 401, "unauthenticated")
	btok, _ := h.browser(idAnn)
	wantErr(t, h.do("DELETE", "/api/auth/code", "", cookie(btok)), 403, "wrong_session")
}

func TestLiveCodeCap(t *testing.T) {
	h := newHarness(t)
	for i := range 100 {
		if _, _, err := h.svc.NewLoginCode(context.Background(), int64(5000+i)); err != nil {
			t.Fatalf("code %d: %v", i+1, err)
		}
	}
	tok := h.signIn(userAnn)
	wantErr(t, h.do("POST", "/api/auth/code", signInBody(h.initData(userAnn)), bearer(tok)), 503, "too_many_codes")
	// After the codes expire, there is room again.
	h.clk.Advance(5*time.Minute + time.Second)
	tok = h.signIn(userAnn)
	h.newCode(tok, userAnn)
}

// TestConcurrentRedeem: many requests with one code, on separate IPs, give
// one session, one notice and one cookie. The store decides the winner.
func TestConcurrentRedeem(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	code := h.newCode(tok, userAnn)
	const n = nodeFailMax // each request reserves one failure before the attempt
	var wg sync.WaitGroup
	codes := make([]int, n)
	cookies := make([]int, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		ip := nextIP()
		go func() {
			defer wg.Done()
			<-start
			w := h.do("POST", "/api/auth/code/redeem", codeBody(code), peer(ip))
			codes[i], cookies[i] = w.Code, len(w.Result().Cookies())
		}()
	}
	close(start)
	wg.Wait()
	h.srv.codes.notices.Wait()
	wins, losses := 0, 0
	for i := range n {
		switch codes[i] {
		case 200:
			wins++
			if cookies[i] != 1 {
				t.Errorf("winner %d got %d cookies", i, cookies[i])
			}
		case 401:
			losses++
			if cookies[i] != 0 {
				t.Errorf("loser %d got a cookie", i)
			}
		default:
			t.Errorf("request %d: status %d", i, codes[i])
		}
	}
	if wins != 1 || losses != n-1 {
		t.Fatalf("%d wins and %d losses", wins, losses)
	}
	if got := len(h.send.Messages()); got != 1 {
		t.Errorf("%d notices, want 1", got)
	}
}

// The owner decisions of #141: 3 failures per IP in 1 hour, and 22 per
// node in 1 hour.
func TestRedeemFailureLimitValues(t *testing.T) {
	if ipFailMax != 3 || ipFailWindow != time.Hour || nodeFailMax != 22 || nodeFailWindow != time.Hour {
		t.Errorf("limits %d/%v per IP, %d/%v per node", ipFailMax, ipFailWindow, nodeFailMax, nodeFailWindow)
	}
}

func TestRedeemFailureLimitPerIP(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	const ip = "198.51.100.7:4000"
	for range 3 {
		wantErr(t, h.redeem("ZZZZ-ZZZZ", peer(ip)), 401, "bad_code")
		h.clk.Advance(time.Minute) // stay below the route limit of 10 a minute
	}
	// The fourth is refused, also with a good code.
	code := h.newCode(tok, userAnn)
	w := h.redeem(code, peer(ip))
	wantErr(t, w, 429, "rate_limited")
	if ra, _ := strconv.Atoi(w.Header().Get("Retry-After")); ra < 1 || ra > 3600 {
		t.Errorf("Retry-After %q", w.Header().Get("Retry-After"))
	}
	// Another IP is free, and the code still works.
	other := h.redeem(code, peer("198.51.100.8:1"))
	if other.Code != 200 {
		t.Fatalf("other IP: %d %s", other.Code, other.Body)
	}
	// The window is 1 hour from the first failure: 10 minutes is not enough.
	h.clk.Advance(10 * time.Minute)
	wantErr(t, h.redeem("ZZZZ-ZZZZ", peer(ip)), 429, "rate_limited")
	// 1 hour after the first failure, one slot is free again.
	h.clk.Advance(time.Hour - 13*time.Minute)
	wantErr(t, h.redeem("ZZZZ-ZZZZ", peer(ip)), 401, "bad_code")
	wantErr(t, h.redeem("ZZZZ-ZZZZ", peer(ip)), 429, "rate_limited")
}

func TestRedeemSuccessDoesNotCount(t *testing.T) {
	h := newHarness(t)
	const ip = "198.51.100.9:4000"
	for i := range 3 * ipFailMax {
		tok := h.signIn(userAnn)
		if w := h.redeem(h.newCode(tok, userAnn), peer(ip)); w.Code != 200 {
			t.Fatalf("redeem %d: %d %s", i+1, w.Code, w.Body)
		}
		h.clk.Advance(3 * time.Minute) // the limit of 5 codes in 10 minutes for each user
	}
}

func TestRedeemFailureLimitPerNode(t *testing.T) {
	h := newHarness(t)
	tok := h.signIn(userAnn)
	for range 22 {
		wantErr(t, h.redeem("ZZZZ-ZZZZ"), 401, "bad_code") // a new IP each time
	}
	// The 23rd is refused for every client, also with a good code.
	code := h.newCode(tok, userAnn)
	for _, ip := range []string{"198.51.100.20:1", "[2001:db8:9::1]:1"} {
		w := h.redeem(code, peer(ip))
		wantErr(t, w, 429, "rate_limited")
		if ra, _ := strconv.Atoi(w.Header().Get("Retry-After")); ra < 1 || ra > 3600 {
			t.Errorf("Retry-After %q", w.Header().Get("Retry-After"))
		}
	}
	// The limit ends with the hour.
	h.clk.Advance(nodeFailWindow)
	tok = h.signIn(userAnn)
	if w := h.redeem(h.newCode(tok, userAnn)); w.Code != 200 {
		t.Fatalf("after the hour: %d %s", w.Code, w.Body)
	}
}

func TestRedeemUserNoLongerAllowed(t *testing.T) {
	h := newHarness(t)
	code, _, err := h.svc.NewLoginCode(context.Background(), idEve)
	if err != nil {
		t.Fatal(err)
	}
	w := h.redeem(code)
	wantErr(t, w, 403, "not_allowed")
	if len(w.Result().Cookies()) != 0 || len(h.send.Messages()) != 0 {
		t.Error("a user who is not allowed got a cookie or a notice")
	}
}

func TestSignInNotice(t *testing.T) {
	h := newHarness(t)
	code := h.newCode(h.signIn(userAnn), userAnn)
	w := h.redeem(code, peer("127.0.0.1:1"), header("CF-Connecting-IP", "203.0.113.50"),
		header("CF-IPCountry", "DE"), header("User-Agent", "<b>Evil</b>"))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	msgs := h.send.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != idAnn {
		t.Fatalf("messages %+v", msgs)
	}
	m := msgs[0].Msg
	if !strings.Contains(m.Text, "DE") || !strings.Contains(m.Text, "203.0.113.50") || strings.Contains(m.Text, "Evil") {
		t.Errorf("text %q", m.Text)
	}
	if !strings.Contains(m.Text, "\u2066DE\u2069") || !strings.Contains(m.Text, "\u2066203.0.113.50\u2069") {
		t.Errorf("the Farsi text does not isolate the values: %q", m.Text)
	}
	want := [][]tg.Button{{{Text: "خروج از همه\u200cجا", WebAppURL: testOrigin + "/tg/?a=signout"}}}
	if len(m.Keyboard) != 1 || len(m.Keyboard[0]) != 1 || m.Keyboard[0][0] != want[0][0] {
		t.Errorf("keyboard %+v, want %+v", m.Keyboard, want)
	}
	// An English user, and a peer that is not a trusted proxy: the headers
	// are not trusted, so the country is unknown.
	bob := h.signIn(userBob)
	h.send.Reset()
	h.redeem(h.newCode(bob, userBob), peer("198.51.100.1:1"), header("CF-IPCountry", "DE"), header("CF-Connecting-IP", "1.2.3.4"))
	m = h.send.Messages()[0].Msg
	if m.Text != "New sign-in to the web app from ?? (198.51.100.1). If this was not you, log out everywhere." ||
		m.Keyboard[0][0].Text != "Log out everywhere" {
		t.Errorf("text %q, button %q", m.Text, m.Keyboard[0][0].Text)
	}
}

func TestSignInNoticeErrorKeepsSession(t *testing.T) {
	h := newHarness(t)
	h.send.ErrFn = func(int64, tg.OutMessage) error { return errors.New("telegram is down") }
	code := h.newCode(h.signIn(userAnn), userAnn)
	w := h.redeem(code)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("cookies %v", cs)
	}
	if me := h.do("GET", "/api/me", "", cookie(cs[0].Value)); me.Code != 200 {
		t.Errorf("the session does not work: %d", me.Code)
	}
	if !strings.Contains(h.log.String(), "sign-in notice") {
		t.Error("no warning in the log")
	}
}

// TestCodeNotInLogOrURL runs the whole flow, with good and wrong codes, and
// checks that the code is in no log line, no header and no URL.
func TestCodeNotInLogOrURL(t *testing.T) {
	h := newHarness(t)
	raw := h.initData(userAnn)
	tok := h.signIn(userAnn)
	code := h.newCode(tok, userAnn)
	plain := strings.Replace(code, "-", "", 1)
	wrong := "QQQQ-RRRR"
	var all []*httptest.ResponseRecorder
	all = append(all, h.redeem(wrong, peer("198.51.100.20:1")))
	all = append(all, h.redeem(strings.ToLower(code)))
	all = append(all, h.redeem(code)) // used: bad_code
	h.do("POST", "/api/auth/code", `{"init_data":"x"}`, bearer(tok))
	h.do("DELETE", "/api/auth/code", "", bearer(tok))
	log := h.log.String()
	if !strings.Contains(log, "route=\"POST /api/auth/code/redeem\"") {
		t.Fatalf("the log lacks the redeem lines: %s", log)
	}
	for name, v := range map[string]string{
		"code": code, "code with no dash": plain, "lower case code": strings.ToLower(plain),
		"wrong code": wrong, "init data": raw, "token": tok,
	} {
		if strings.Contains(log, v) {
			t.Errorf("the log holds the %s", name)
		}
	}
	for _, w := range all {
		for k, vs := range w.Header() {
			for _, v := range vs {
				if strings.Contains(v, plain) && k != "Set-Cookie" {
					t.Errorf("header %s holds the code", k)
				}
			}
		}
	}
	// The routes take the code in the body only: a code in the query or in
	// the path is not read.
	wantErr(t, h.do("POST", "/api/auth/code/redeem?code="+plain, "", peer(nextIP()), delHeader("Content-Type")), 400, "bad_request")
	if w := h.do("POST", "/api/auth/code/redeem/"+plain, "", peer(nextIP())); w.Code == 200 {
		t.Error("a code in the path signed in")
	}
}

func TestCodeStateReserve(t *testing.T) {
	var c codeState
	c.byIP = map[string][]time.Time{}
	now := testNow
	for range ipFailMax {
		if ok, _ := c.reserve("a", now); !ok {
			t.Fatal("refused too early")
		}
	}
	if ok, retry := c.reserve("a", now); ok || retry != ipFailWindow {
		t.Errorf("one above the limit: %v %v", ok, retry)
	}
	c.release("a", now)
	if ok, _ := c.reserve("a", now); !ok {
		t.Error("release did not free a slot")
	}
	if ok, _ := c.reserve("b", now.Add(ipFailWindow)); !ok {
		t.Error("another IP is refused")
	}
}

// The route limit of 10 requests a minute for each IP comes before the
// handler. A request with an empty code is not a failed guess.
func TestRedeemRouteLimit(t *testing.T) {
	h := newHarness(t)
	for range redeemPerMin {
		wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":""}`, peer("10.7.7.7:1")), 400, "bad_request")
	}
	wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":""}`, peer("10.7.7.7:1")), 429, "rate_limited")
}

func TestFailKeyIPv6Prefix(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":                   "203.0.113.9",
		"::ffff:203.0.113.9":            "::ffff:203.0.113.9",
		"2001:db8:1:2:aaaa:bbbb:c:d":    "2001:db8:1:2::",
		"2001:db8:1:2:ffff:ffff:ffff:1": "2001:db8:1:2::",
	} {
		if got := failKey(netip.MustParseAddr(in)); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

// Addresses of one /64 share the failure limit; another /64 does not.
func TestRedeemFailureLimitIPv6Prefix(t *testing.T) {
	h := newHarness(t)
	for i := range 3 {
		wantErr(t, h.redeem("ZZZZ-ZZZZ", peer("[2001:db8:1:2::"+strconv.Itoa(i+1)+"]:1")), 401, "bad_code")
	}
	wantErr(t, h.redeem("ZZZZ-ZZZZ", peer("[2001:db8:1:2:ffff::9]:1")), 429, "rate_limited")
	wantErr(t, h.redeem("ZZZZ-ZZZZ", peer("[2001:db8:1:3::1]:1")), 401, "bad_code")
}

// IPv4 and IPv4-mapped addresses keep a key for each address.
func TestRedeemFailureLimitIPv4PerAddress(t *testing.T) {
	h := newHarness(t)
	for range 3 {
		wantErr(t, h.redeem("ZZZZ-ZZZZ", peer("198.51.100.30:1")), 401, "bad_code")
	}
	wantErr(t, h.redeem("ZZZZ-ZZZZ", peer("198.51.100.30:1")), 429, "rate_limited")
	wantErr(t, h.redeem("ZZZZ-ZZZZ", peer("198.51.100.31:1")), 401, "bad_code")
	wantErr(t, h.redeem("ZZZZ-ZZZZ", peer("[::ffff:198.51.100.32]:1")), 401, "bad_code")
}

// The route limit of 10 requests a minute uses the same /64 key as the
// failure limit. A request with an empty code is not a failed guess.
func TestRedeemRouteLimitIPv6Prefix(t *testing.T) {
	h := newHarness(t)
	for i := range redeemPerMin {
		ip := "[2001:db8:5:6::" + strconv.Itoa(i+1) + "]:1"
		wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":""}`, peer(ip)), 400, "bad_request")
	}
	wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":""}`, peer("[2001:db8:5:6:ffff::1]:1")), 429, "rate_limited")
	wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":""}`, peer("[2001:db8:5:7::1]:1")), 400, "bad_request")
}

// The route limit keeps a key for each IPv4 address.
func TestRedeemRouteLimitIPv4PerAddress(t *testing.T) {
	h := newHarness(t)
	for range redeemPerMin {
		wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":""}`, peer("10.8.8.8:1")), 400, "bad_request")
	}
	wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":""}`, peer("10.8.8.8:1")), 429, "rate_limited")
	wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":""}`, peer("10.8.8.9:1")), 400, "bad_request")
	wantErr(t, h.do("POST", "/api/auth/code/redeem", `{"code":""}`, peer("[::ffff:10.8.8.10]:1")), 400, "bad_request")
}

// A slow Telegram does not hold the response.
func TestSignInNoticeDoesNotBlockResponse(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	h.send.ErrFn = func(int64, tg.OutMessage) error { <-release; return nil }
	code := h.newCode(h.signIn(userAnn), userAnn)
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- h.do("POST", "/api/auth/code/redeem", codeBody(code), peer(nextIP())) }()
	select {
	case w := <-done:
		if w.Code != 200 {
			t.Errorf("%d", w.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the response waits for the notice")
	}
	close(release)
	h.srv.codes.notices.Wait()
}

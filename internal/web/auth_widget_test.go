package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

func TestWidgetStringsBothLanguages(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
		other := i18n.Fa
		if lang == i18n.Fa {
			other = i18n.En
		}
		for _, k := range i18n.Keys(lang) {
			if !strings.HasPrefix(k, "web.widget.") {
				continue
			}
			if !hasKey(other, k) {
				t.Errorf("%s has no %s string", k, other)
			}
			if s := (i18n.Locale{Lang: lang}).T(k); strings.Contains(s, "%") {
				t.Errorf("%s (%s) has a verb: %q", k, lang, s)
			}
		}
	}
	// Each code of the redirect has its text.
	for _, c := range []string{widgetBadRequest, widgetBadSignature, widgetExpired, widgetNotAllowed, widgetUsed, widgetInternal, widgetRestart, widgetOtherUser} {
		for _, lang := range []i18n.Lang{i18n.En, i18n.Fa} {
			if !hasKey(lang, "web.widget.error_"+c) {
				t.Errorf("no %s string for web.widget.error_%s", lang, c)
			}
		}
	}
	if !hasKey(i18n.En, "web.widget.hint") {
		t.Error("the login screen needs web.widget.hint")
	}
}

// widgetFields are the fields that Telegram sends for user id, made at authDate.
func widgetFields(id int64, authDate time.Time) map[string]string {
	return map[string]string{
		"id":         strconv.FormatInt(id, 10),
		"first_name": "Ann",
		"last_name":  "Lee",
		"username":   "ann",
		"photo_url":  "https://t.me/i/userpic/320/ann.jpg",
		"auth_date":  strconv.FormatInt(authDate.Unix(), 10),
	}
}

// signWidget signs fields as Telegram does for the Login Widget
// (https://core.telegram.org/widgets/login#checking-authorization): the
// secret key is SHA256(token), and the hash is the hex HMAC-SHA256 of the
// sorted "key=value" lines. It returns the query of the redirect.
func signWidget(token string, f map[string]string) string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + f[k]
	}
	secret := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(strings.Join(lines, "\n")))
	q := url.Values{}
	for k, v := range f {
		q.Set(k, v)
	}
	q.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return q.Encode()
}

// testNonce is a nonce of the right form, as the start request gives.
var testNonce = strings.Repeat("N", widgetNonceLen)

func nonce(v string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: widgetCookie, Value: v}) }
}

// widget sends the redirect of Telegram to endpoint 24, from a new IP, from
// a browser that started the widget (it has the nonce cookie).
func (h *harness) widget(query string, opts ...reqOpt) *httptest.ResponseRecorder {
	h.t.Helper()
	defer h.srv.codes.notices.Wait() // the notice goes out in the background
	return h.do("GET", "/auth/telegram?"+query, "", append([]reqOpt{peer(nextIP()), nonce(testNonce)}, opts...)...)
}

// cookiesOf returns the cookies that w sets, by name.
func cookiesOf(w *httptest.ResponseRecorder) map[string]*http.Cookie {
	m := map[string]*http.Cookie{}
	for _, c := range w.Result().Cookies() {
		m[c.Name] = c
	}
	return m
}

// wantNonceCleared checks that w deletes the nonce cookie.
func wantNonceCleared(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	c := cookiesOf(w)[widgetCookie]
	if c == nil || c.MaxAge >= 0 || c.Value != "" {
		t.Errorf("the nonce is not cleared: %+v", c)
	}
}

// wantLoginError checks a 303 to /#login-error=<code> with no cookie.
func wantLoginError(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303; body %s", w.Code, w.Body)
	}
	if got, want := w.Header().Get("Location"), "/#login-error="+code; got != want {
		t.Fatalf("Location %q, want %q", got, want)
	}
	if c := cookiesOf(w)[cookieName]; c != nil {
		t.Errorf("an error sets the session cookie: %+v", c)
	}
	wantNonceCleared(t, w)
}

func TestWidgetSignsIn(t *testing.T) {
	h := newHarness(t)
	w := h.widget(signWidget(testToken, widgetFields(idAnn, h.clk.Now())), header("User-Agent", "Firefox"))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("%d, Location %q, body %s", w.Code, w.Header().Get("Location"), w.Body)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q", cc)
	}
	wantNonceCleared(t, w)
	sc := cookiesOf(w)[cookieName]
	if len(w.Result().Cookies()) != 2 || sc == nil || !tokenRE.MatchString(sc.Value) ||
		!sc.Secure || !sc.HttpOnly || sc.SameSite != http.SameSiteStrictMode || sc.Path != "/" {
		t.Fatalf("cookies %+v", w.Result().Cookies())
	}
	cs := []*http.Cookie{sc}
	ws, err := h.svc.WebSession(context.Background(), cs[0].Value, time.Hour)
	if err != nil || ws.Kind != model.WebBrowser || ws.UserID != idAnn || ws.Device != "Firefox" {
		t.Fatalf("session %+v, %v", ws, err)
	}
	if want := h.clk.Now().Add(testConfig().WebSessionTTL); !ws.ExpiresAt.Equal(want) {
		t.Errorf("expires_at %v, want %v", ws.ExpiresAt, want)
	}
	if me := h.do("GET", "/api/me", "", cookie(cs[0].Value)); me.Code != 200 {
		t.Errorf("/api/me with the cookie: %d", me.Code)
	}
	// The sign-in notice of §3.3.1 goes to the private chat of the user.
	msgs := h.send.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != idAnn || !strings.Contains(msgs[0].Msg.Text, "New sign-in") ||
		msgs[0].Msg.Keyboard[0][0].WebAppURL != testOrigin+"/tg/?a=signout" {
		t.Fatalf("notices %+v", msgs)
	}
	// The user exists now, with the username of the widget.
	users, err := h.svc.NamedUsers(context.Background(), 0)
	if err != nil || len(users) != 1 || users[0].ID != idAnn || users[0].Username != "ann" {
		t.Errorf("named users %+v, %v", users, err)
	}
}

func TestWidgetEachSignInSendsTheNotice(t *testing.T) {
	h := newHarness(t)
	for i := range 3 {
		h.clk.Advance(time.Second) // a new proof each time
		if w := h.widget(signWidget(testToken, widgetFields(idBob, h.clk.Now()))); w.Code != 303 ||
			w.Header().Get("Location") != "/" {
			t.Fatalf("sign-in %d: %d %q", i+1, w.Code, w.Header().Get("Location"))
		}
	}
	if n := len(h.send.Messages()); n != 3 {
		t.Errorf("%d notices, want 3", n)
	}
}

func TestWidgetProofWorksOneTime(t *testing.T) {
	h := newHarness(t)
	q := signWidget(testToken, widgetFields(idAnn, h.clk.Now()))
	if w := h.widget(q); w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatalf("first: %d %q", w.Code, w.Header().Get("Location"))
	}
	wantLoginError(t, h.widget(q), "used")
	// The same proof with the hash in upper case passes the HMAC, but it is
	// still the same proof.
	vals, _ := url.ParseQuery(q)
	vals.Set("hash", strings.ToUpper(vals.Get("hash")))
	wantLoginError(t, h.widget(vals.Encode()), "used")
	// Later, but inside the age limit.
	h.clk.Advance(9 * time.Minute)
	wantLoginError(t, h.widget(q), "used")
	if n := len(h.send.Messages()); n != 1 {
		t.Errorf("%d notices, want 1", n)
	}
}

func TestWidgetConcurrentProof(t *testing.T) {
	h := newHarness(t)
	q := signWidget(testToken, widgetFields(idAnn, h.clk.Now()))
	const n = 8
	var wg sync.WaitGroup
	locs := make([]string, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		ip := nextIP()
		go func() {
			defer wg.Done()
			<-start
			w := h.do("GET", "/auth/telegram?"+q, "", peer(ip), nonce(testNonce))
			locs[i] = w.Header().Get("Location")
		}()
	}
	close(start)
	wg.Wait()
	h.srv.codes.notices.Wait()
	ok := 0
	for _, l := range locs {
		switch l {
		case "/":
			ok++
		case "/#login-error=used":
		default:
			t.Errorf("Location %q", l)
		}
	}
	if ok != 1 {
		t.Errorf("%d sign-ins, want 1", ok)
	}
	if got := len(h.send.Messages()); got != 1 {
		t.Errorf("%d notices, want 1", got)
	}
}

func TestWidgetAgeLimit(t *testing.T) {
	h := newHarness(t)
	now := h.clk.Now()
	if w := h.widget(signWidget(testToken, widgetFields(idAnn, now.Add(-widgetProofAge+time.Second)))); w.Code != 303 ||
		w.Header().Get("Location") != "/" {
		t.Fatalf("proof of 9:59: %d %q", w.Code, w.Header().Get("Location"))
	}
	// At 10 minutes the store would already delete the row, so the proof
	// must stop there too.
	wantLoginError(t, h.widget(signWidget(testToken, widgetFields(idAnn, now.Add(-widgetProofAge)))), "expired")
	wantLoginError(t, h.widget(signWidget(testToken, widgetFields(idAnn, now.Add(-time.Hour)))), "expired")
	// The future: 60 s of clock skew is fine, more is not.
	if w := h.widget(signWidget(testToken, widgetFields(idAnn, now.Add(time.Minute)))); w.Header().Get("Location") != "/" {
		t.Errorf("proof 60 s ahead: %q", w.Header().Get("Location"))
	}
	wantLoginError(t, h.widget(signWidget(testToken, widgetFields(idAnn, now.Add(time.Minute+time.Second)))), "expired")
}

// TestWidgetUsedProofAfterExpiry: a proof that the store forgot (after its
// expiry) is too old for the age check, so it never works again.
func TestWidgetUsedProofAfterExpiry(t *testing.T) {
	h := newHarness(t)
	q := signWidget(testToken, widgetFields(idAnn, h.clk.Now()))
	if w := h.widget(q); w.Header().Get("Location") != "/" {
		t.Fatalf("first: %q", w.Header().Get("Location"))
	}
	h.clk.Advance(widgetProofAge)
	wantLoginError(t, h.widget(q), "expired")
}

func TestWidgetSignature(t *testing.T) {
	h := newHarness(t)
	f := widgetFields(idAnn, h.clk.Now())
	good := signWidget(testToken, f)
	mutate := func(fn func(url.Values)) string {
		v, _ := url.ParseQuery(good)
		fn(v)
		return v.Encode()
	}
	// The Mini App key (HMAC with "WebAppData") is not the widget key.
	miniKey := func() string {
		keys := make([]string, 0, len(f))
		for k := range f {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		lines := make([]string, len(keys))
		for i, k := range keys {
			lines[i] = k + "=" + f[k]
		}
		s := hmac.New(sha256.New, []byte("WebAppData"))
		s.Write([]byte(testToken))
		mac := hmac.New(sha256.New, s.Sum(nil))
		mac.Write([]byte(strings.Join(lines, "\n")))
		return mutate(func(v url.Values) { v.Set("hash", hex.EncodeToString(mac.Sum(nil))) })
	}()
	hash, _ := url.ParseQuery(good)
	for name, q := range map[string]string{
		"other token":   signWidget("1:OTHER", f),
		"mini app key":  miniKey,
		"changed id":    mutate(func(v url.Values) { v.Set("id", strconv.Itoa(idBob)) }),
		"changed name":  mutate(func(v url.Values) { v.Set("username", "eve") }),
		"removed field": mutate(func(v url.Values) { v.Del("photo_url") }),
		"no hash":       mutate(func(v url.Values) { v.Del("hash") }),
		"empty hash":    mutate(func(v url.Values) { v.Set("hash", "") }),
		"non-hex hash":  mutate(func(v url.Values) { v.Set("hash", strings.Repeat("zz", 32)) }),
		"short hash":    mutate(func(v url.Values) { v.Set("hash", hash.Get("hash")[:10]) }),
	} {
		t.Run(name, func(t *testing.T) { wantLoginError(t, h.widget(q), "bad_signature") })
	}
	if len(h.send.Messages()) != 0 {
		t.Error("a bad proof sent a notice")
	}
}

func TestWidgetBadRequest(t *testing.T) {
	h := newHarness(t)
	now := h.clk.Now()
	noID := widgetFields(idAnn, now)
	delete(noID, "id")
	noDate := widgetFields(idAnn, now)
	delete(noDate, "auth_date")
	badDate := widgetFields(idAnn, now)
	badDate["auth_date"] = "soon"
	for name, q := range map[string]string{
		"nothing":      "",
		"bad escape":   "id=%zz&hash=00",
		"duplicate":    signWidget(testToken, widgetFields(idAnn, now)) + "&id=43",
		"no id":        signWidget(testToken, noID),
		"zero id":      signWidget(testToken, widgetFields(0, now)),
		"negative id":  signWidget(testToken, widgetFields(-5, now)),
		"no auth_date": signWidget(testToken, noDate),
		"bad date":     signWidget(testToken, badDate),
		"too long":     signWidget(testToken, widgetFields(idAnn, now)) + "&x=" + strings.Repeat("a", maxWidgetQuery),
	} {
		t.Run(name, func(t *testing.T) { wantLoginError(t, h.widget(q), "bad_request") })
	}
}

func TestWidgetNotAllowed(t *testing.T) {
	h := newHarness(t)
	q := signWidget(testToken, widgetFields(idEve, h.clk.Now()))
	w := h.widget(q)
	// The user ID is a received field, so it stays out of the URL.
	wantLoginError(t, w, "not_allowed")
	if len(h.send.Messages()) != 0 {
		t.Error("a user who is not allowed got a notice")
	}
	// The refusal does not use the proof: after the owner adds the user and
	// restarts, the same proof (if still fresh) still works.
	if fresh, err := h.svc.UseProof(context.Background(), proofKey(mustHash(t, q)), h.clk.Now().Add(time.Minute)); err != nil || !fresh {
		t.Errorf("the refusal used the proof: fresh=%v, %v", fresh, err)
	}
}

func mustHash(t *testing.T, q string) []byte {
	t.Helper()
	v, _ := url.ParseQuery(q)
	b, err := hex.DecodeString(v.Get("hash"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWidgetRateLimitPerIP(t *testing.T) {
	h := newHarness(t)
	for i := range widgetPerMin {
		if w := h.do("GET", "/auth/telegram", "", peer("10.7.7.7:1")); w.Code != 303 {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	wantErr(t, h.do("GET", "/auth/telegram", "", peer("10.7.7.7:1")), 429, "rate_limited")
}

// TestWidgetKeepsFieldsOutOfLog: the query holds a proof, so neither the log
// nor the redirect holds any received field.
func TestWidgetKeepsFieldsOutOfLog(t *testing.T) {
	h := newHarness(t)
	q := signWidget(testToken, widgetFields(idAnn, h.clk.Now()))
	v, _ := url.ParseQuery(q)
	h.widget(q)
	w := h.widget(q)
	h.widget(signWidget("1:OTHER", widgetFields(idAnn, h.clk.Now())))
	log := h.log.String()
	if !strings.Contains(log, `route="GET /auth/telegram"`) {
		t.Fatalf("no request line: %s", log)
	}
	for _, s := range []string{v.Get("hash"), v.Get("auth_date"), "userpic", "first_name", "Lee", testToken} {
		if strings.Contains(log, s) {
			t.Errorf("the log holds %q", s)
		}
		if strings.Contains(w.Header().Get("Location"), s) {
			t.Errorf("the redirect holds %q", s)
		}
	}
}

// TestWidgetOnlyKnownFields: the check string and the user come from the
// documented fields only. A signed proof with any other key, or with a line
// feed in a key or a value, is refused.
func TestWidgetOnlyKnownFields(t *testing.T) {
	h := newHarness(t)
	now := h.clk.Now()
	with := func(k, v string) string {
		f := widgetFields(idAnn, now)
		f[k] = v
		return signWidget(testToken, f)
	}
	for name, q := range map[string]string{
		"extra field":       with("extra", "1"),
		"user field":        with("user", `{"id":43}`),
		"line feed in name": with("first_name", "Ann\nid=43"),
		"return in name":    with("last_name", "Lee\r"),
		"line feed in key":  with("first_name\nid", "43"),
		"signature field":   with("signature", "x"),
	} {
		t.Run(name, func(t *testing.T) { wantLoginError(t, h.widget(q), "bad_request") })
	}
	// Only the documented fields, some of them missing: it works.
	f := widgetFields(idBob, now)
	delete(f, "last_name")
	delete(f, "photo_url")
	delete(f, "username")
	if w := h.widget(signWidget(testToken, f)); w.Header().Get("Location") != "/" {
		t.Errorf("minimal fields: %q", w.Header().Get("Location"))
	}
}

// TestWidgetNeedsTheNonce: a link with a fresh proof does not sign in a
// browser that did not start the widget (login CSRF).
func TestWidgetNeedsTheNonce(t *testing.T) {
	h := newHarness(t)
	q := signWidget(testToken, widgetFields(idAnn, h.clk.Now()))
	for name, opt := range map[string]reqOpt{
		"no nonce":   func(*http.Request) {},
		"empty":      nonce(""),
		"short":      nonce("abc"),
		"not base64": nonce(strings.Repeat("*", widgetNonceLen)),
		"too long":   nonce(strings.Repeat("N", widgetNonceLen+1)),
	} {
		t.Run(name, func(t *testing.T) {
			wantLoginError(t, h.do("GET", "/auth/telegram?"+q, "", peer(nextIP()), opt), "restart")
		})
	}
	// The refusals did not use the proof.
	if w := h.widget(q); w.Header().Get("Location") != "/" {
		t.Errorf("with the nonce: %q", w.Header().Get("Location"))
	}
	if n := len(h.send.Messages()); n != 1 {
		t.Errorf("%d notices, want 1", n)
	}
}

func TestWidgetStart(t *testing.T) {
	h := newHarness(t)
	start := func(opts ...reqOpt) *httptest.ResponseRecorder {
		base := []reqOpt{peer(nextIP()), header("X-Todo-Request", "1"), header("Sec-Fetch-Site", "same-origin")}
		return h.do("GET", "/auth/telegram?start=1", "", append(base, opts...)...)
	}
	w := start()
	if w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	c := cookiesOf(w)[widgetCookie]
	if c == nil || !validNonce(c.Value) || c.MaxAge != 600 || !c.Secure || !c.HttpOnly ||
		c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" {
		t.Fatalf("nonce cookie %+v", c)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q", cc)
	}
	if other := cookiesOf(start())[widgetCookie]; other == nil || other.Value == c.Value {
		t.Error("two nonces are equal")
	}
	// The nonce of the start request works for the redirect.
	q := signWidget(testToken, widgetFields(idAnn, h.clk.Now()))
	if w := h.do("GET", "/auth/telegram?"+q, "", peer(nextIP()), nonce(c.Value)); w.Header().Get("Location") != "/" {
		t.Errorf("redirect with the nonce: %q", w.Header().Get("Location"))
	}
	h.srv.codes.notices.Wait()

	// A cross-site page cannot get a nonce: no custom header, or not same-origin.
	for name, opts := range map[string][]reqOpt{
		"no header":  {delHeader("X-Todo-Request")},
		"cross-site": {header("Sec-Fetch-Site", "cross-site")},
		"same-site":  {header("Sec-Fetch-Site", "same-site")},
		"no fetch":   {delHeader("Sec-Fetch-Site")},
	} {
		t.Run(name, func(t *testing.T) {
			w := start(opts...)
			wantErr(t, w, 403, "csrf")
			if cookiesOf(w)[widgetCookie] != nil {
				t.Error("a nonce went out")
			}
		})
	}
	// A browser that is signed in gets no nonce. A dead session does not count.
	btok, _ := h.browser(idBob)
	w = start(cookie(btok))
	wantErr(t, w, 403, "wrong_session")
	if cookiesOf(w)[widgetCookie] != nil {
		t.Error("a signed-in browser got a nonce")
	}
	if w := start(cookie("tb1_" + strings.Repeat("Z", 43))); w.Code != http.StatusNoContent {
		t.Errorf("with a dead session: %d", w.Code)
	}
}

// TestWidgetKeepsTheSessionOfAnotherUser: a browser that sends the session
// of another user keeps it. The same user can sign in again.
func TestWidgetKeepsTheSessionOfAnotherUser(t *testing.T) {
	h := newHarness(t)
	btok, _ := h.browser(idBob)
	wantLoginError(t, h.widget(signWidget(testToken, widgetFields(idAnn, h.clk.Now())), cookie(btok)), "other_user")
	if !live(h, btok) {
		t.Error("the session of Bob ended")
	}
	if len(h.send.Messages()) != 0 {
		t.Error("a notice went out")
	}
	h.clk.Advance(time.Second)
	if w := h.widget(signWidget(testToken, widgetFields(idBob, h.clk.Now())), cookie(btok)); w.Header().Get("Location") != "/" {
		t.Errorf("the same user: %q", w.Header().Get("Location"))
	}
}

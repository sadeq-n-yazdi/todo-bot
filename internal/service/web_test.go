package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

const (
	idle30d = 30 * 24 * time.Hour
	max90d  = 90 * 24 * time.Hour
	mini12h = 12 * time.Hour
)

var (
	tokenRE = regexp.MustCompile(`^tb1_[A-Za-z0-9_-]{43}$`)
	hashRE  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	codeRE  = regexp.MustCompile(`^[0-9ABCDEFGHJKMNPQRSTVWXYZ]{8}$`)
)

func (e *env) startSession(user int64, kind model.WebSessionKind, idle, maxLife time.Duration) (string, model.WebSession) {
	e.t.Helper()
	tok, s, err := e.svc.StartWebSession(ctx, user, kind, "Firefox", idle, maxLife)
	e.must(err)
	return tok, s
}

func (e *env) live(token string) bool {
	e.t.Helper()
	_, err := e.svc.WebSession(ctx, token, idle30d)
	if err != nil && !errors.Is(err, ErrNotFound) {
		e.t.Fatal(err)
	}
	return err == nil
}

// raw opens the database file directly, outside the store.
func (e *env) raw() *sql.DB {
	e.t.Helper()
	u, err := url.Parse(e.url)
	if err != nil {
		e.t.Fatal(err)
	}
	// url.URL escapes "#", "?" and "%" in the path, so SQLite reads them as
	// part of the file name and not as a fragment or a query.
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: u.Path}).String())
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = db.Close() })
	return db
}

func (e *env) column(query string) []string {
	e.t.Helper()
	rows, err := e.raw().Query(query)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		e.must(rows.Scan(&v))
		out = append(out, v)
	}
	e.must(rows.Err())
	return out
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestStartWebSessionTokenAndRow(t *testing.T) {
	e := newEnv(t)
	tok, s := e.startSession(7, model.WebBrowser, idle30d, max90d)
	if !tokenRE.MatchString(tok) || len(tok) != 47 {
		t.Fatalf("token %q has the wrong form", tok)
	}
	want := model.WebSession{ID: sha(tok), UserID: 7, Kind: model.WebBrowser, Device: "Firefox",
		CreatedAt: t0, SeenAt: t0, ExpiresAt: t0.Add(idle30d), MaxAt: t0.Add(max90d)}
	if s != want {
		t.Fatalf("session:\n got %+v\nwant %+v", s, want)
	}
	got, err := e.svc.WebSession(ctx, tok, idle30d)
	e.must(err)
	if got != want {
		t.Fatalf("lookup:\n got %+v\nwant %+v", got, want)
	}
	tok2, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)
	if tok2 == tok {
		t.Fatal("two sessions got the same token")
	}
}

// raw must open the file of the store, also when the path of t.TempDir()
// holds characters that have a meaning in a file: URI.
func TestRawOpensTheStoreFile(t *testing.T) {
	t.Run("name with # and ?", func(t *testing.T) {
		e := newEnv(t)
		tok, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)
		if ids := e.column("SELECT id FROM web_sessions"); len(ids) != 1 || ids[0] != sha(tok) {
			t.Fatalf("web_sessions.id = %v, want the hash of the token", ids)
		}
	})
}

func TestWebSessionStoresOnlyHashes(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.startSession(7, model.WebMiniApp, mini12h, mini12h)
	code, _, err := e.svc.NewLoginCode(ctx, 7)
	e.must(err)

	ids := e.column("SELECT id FROM web_sessions")
	if len(ids) != 1 || !hashRE.MatchString(ids[0]) || ids[0] != sha(tok) {
		t.Fatalf("web_sessions.id = %v, want only the hash of the token", ids)
	}
	hashes := e.column("SELECT code_hash FROM web_login_codes")
	if len(hashes) != 1 || !hashRE.MatchString(hashes[0]) || hashes[0] != sha(code) {
		t.Fatalf("web_login_codes.code_hash = %v, want only the hash of the code", hashes)
	}
	// No column of any web row holds the token or the code.
	for _, q := range []string{
		"SELECT id || '|' || device FROM web_sessions",
		"SELECT code_hash FROM web_login_codes",
	} {
		for _, v := range e.column(q) {
			if strings.Contains(v, tok) || strings.Contains(v, code) || strings.Contains(v, tok[4:]) {
				t.Fatalf("%s: a row holds the secret: %q", q, v)
			}
		}
	}
	// The store does not know the token itself.
	if _, err := e.st.GetWebSession(ctx, tok, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("store lookup by the plain token: %v", err)
	}
}

func TestWebSessionRefusesBadTokens(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)
	for _, bad := range []string{"", "tb1_", tok[:46], tok + "A", "tb2_" + tok[4:], sha(tok), " " + tok[1:]} {
		if _, err := e.svc.WebSession(ctx, bad, idle30d); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v, want ErrNotFound", bad, err)
		}
	}
	// A token of the right form that was never issued.
	other := "tb1_" + strings.Repeat("A", 43)
	if _, err := e.svc.WebSession(ctx, other, idle30d); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown token: %v", err)
	}
}

func TestStartWebSessionRefusesBadArgs(t *testing.T) {
	e := newEnv(t)
	for name, f := range map[string]func() error{
		"kind": func() error {
			_, _, err := e.svc.StartWebSession(ctx, 7, "phone", "", time.Hour, time.Hour)
			return err
		},
		"idle": func() error {
			_, _, err := e.svc.StartWebSession(ctx, 7, model.WebBrowser, "", 0, time.Hour)
			return err
		},
		"max": func() error {
			_, _, err := e.svc.StartWebSession(ctx, 7, model.WebBrowser, "", time.Hour, 0)
			return err
		},
	} {
		if f() == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestStartWebSessionCapsExpiryAndDevice(t *testing.T) {
	e := newEnv(t)
	long := strings.Repeat("é", 200)
	_, s, err := e.svc.StartWebSession(ctx, 7, model.WebBrowser, long, 48*time.Hour, 24*time.Hour)
	e.must(err)
	if !s.ExpiresAt.Equal(t0.Add(24*time.Hour)) || !s.MaxAt.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("expiry past max_at: %+v", s)
	}
	if n := len([]rune(s.Device)); n != 120 {
		t.Fatalf("device has %d characters, want 120", n)
	}
}

func TestBrowserSessionSlidesOncePerHour(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)

	e.clk.Advance(59 * time.Minute)
	s, err := e.svc.WebSession(ctx, tok, idle30d)
	e.must(err)
	if !s.SeenAt.Equal(t0) || !s.ExpiresAt.Equal(t0.Add(idle30d)) {
		t.Fatalf("written before 1 h: %+v", s)
	}

	e.clk.Advance(time.Minute)
	now := e.clk.Now()
	s, err = e.svc.WebSession(ctx, tok, idle30d)
	e.must(err)
	if !s.SeenAt.Equal(now) || !s.ExpiresAt.Equal(now.Add(idle30d)) {
		t.Fatalf("no write at 1 h: %+v", s)
	}
	// The write is in the database, not only in the returned value.
	row, err := e.st.GetWebSession(ctx, sha(tok), now)
	e.must(err)
	if row != s {
		t.Fatalf("stored %+v, returned %+v", row, s)
	}
}

func TestBrowserSessionStopsAtMax(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)
	maxAt := t0.Add(max90d)
	for range 4 { // 4 × 25 days passes the 90-day limit
		e.clk.Advance(25 * 24 * time.Hour)
		if e.clk.Now().Before(maxAt) {
			s, err := e.svc.WebSession(ctx, tok, idle30d)
			e.must(err)
			if s.ExpiresAt.After(maxAt) {
				t.Fatalf("expiry %v passes max_at %v", s.ExpiresAt, maxAt)
			}
			if !s.MaxAt.Equal(maxAt) {
				t.Fatalf("max_at moved: %v", s.MaxAt)
			}
		}
	}
	e.clk.Set(maxAt.Add(-time.Second))
	if !e.live(tok) {
		t.Fatal("session gone before max_at")
	}
	e.clk.Set(maxAt)
	if e.live(tok) {
		t.Fatal("session live at max_at")
	}
}

func TestBrowserSessionIdleExpiry(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)
	e.clk.Advance(idle30d - time.Second)
	if !e.live(tok) {
		t.Fatal("session gone before the idle limit")
	}
	// The read above slid the expiry to now+30d.
	e.clk.Advance(idle30d - time.Second)
	if !e.live(tok) {
		t.Fatal("slide did not extend the session")
	}
	e.clk.Advance(idle30d)
	if e.live(tok) {
		t.Fatal("session live after the idle limit")
	}
}

func TestMiniAppSessionNeverSlides(t *testing.T) {
	e := newEnv(t)
	tok, s0 := e.startSession(7, model.WebMiniApp, mini12h, mini12h)
	for range 11 {
		e.clk.Advance(time.Hour)
		s, err := e.svc.WebSession(ctx, tok, mini12h)
		e.must(err)
		if s != s0 {
			t.Fatalf("mini app session changed: %+v", s)
		}
	}
	e.clk.Advance(time.Hour)
	if _, err := e.svc.WebSession(ctx, tok, mini12h); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mini app session live after 12 h: %v", err)
	}
}

func TestSessionLimitsPerKind(t *testing.T) {
	e := newEnv(t)
	var browser, mini []string
	for range 20 {
		tok, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)
		browser = append(browser, tok)
		e.clk.Advance(time.Second) // so that "oldest" is well defined
	}
	other, _ := e.startSession(8, model.WebMiniApp, mini12h, mini12h)
	// Many Mini App starts, all in the same second, never remove a browser
	// session, and always keep the newest one.
	for i := range 8 {
		tok, _ := e.startSession(7, model.WebMiniApp, mini12h, mini12h)
		mini = append(mini, tok)
		if !e.live(tok) {
			t.Fatalf("mini app session %d removed at once", i)
		}
	}
	count := func(toks []string) int {
		n := 0
		for _, tok := range toks {
			if e.live(tok) {
				n++
			}
		}
		return n
	}
	if n := count(mini); n != 5 {
		t.Fatalf("%d mini app sessions, want 5", n)
	}
	if n := count(browser); n != 20 {
		t.Fatalf("mini app starts removed browser sessions: %d left", n)
	}
	if !e.live(other) {
		t.Fatal("the limit removed a session of another user")
	}
	// The 21st browser session removes the oldest one.
	newest, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)
	if !e.live(newest) || e.live(browser[0]) || count(browser[1:]) != 19 {
		t.Fatalf("browser limit: newest live=%v, oldest live=%v, others %d",
			e.live(newest), e.live(browser[0]), count(browser[1:]))
	}
	if n := count(mini); n != 5 {
		t.Fatalf("browser start changed mini app sessions: %d", n)
	}
}

func TestEndWebSessions(t *testing.T) {
	e := newEnv(t)
	a, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)
	b, _ := e.startSession(7, model.WebMiniApp, mini12h, mini12h)
	c, _ := e.startSession(8, model.WebBrowser, idle30d, max90d)

	e.must(e.svc.EndWebSession(ctx, a))
	e.must(e.svc.EndWebSession(ctx, a)) // a second logout is not an error
	e.must(e.svc.EndWebSession(ctx, "garbage"))
	if e.live(a) || !e.live(b) || !e.live(c) {
		t.Fatalf("after logout: a=%v b=%v c=%v", e.live(a), e.live(b), e.live(c))
	}
	e.must(e.svc.EndUserWebSessions(ctx, 7))
	if e.live(b) || !e.live(c) {
		t.Fatalf("after log out everywhere: b=%v c=%v", e.live(b), e.live(c))
	}
}

func TestPruneWebSessions(t *testing.T) {
	e := newEnv(t)
	a, _ := e.startSession(7, model.WebBrowser, idle30d, max90d)
	b, _ := e.startSession(8, model.WebBrowser, idle30d, max90d)
	c, _ := e.startSession(9, model.WebMiniApp, mini12h, mini12h)
	n, err := e.svc.PruneWebSessions(ctx, map[int64]bool{7: true, 9: true, 10: true, 8: false})
	e.must(err)
	if n != 1 || !e.live(a) || e.live(b) || !e.live(c) {
		t.Fatalf("prune: n=%d a=%v b=%v c=%v", n, e.live(a), e.live(b), e.live(c))
	}
	n, err = e.svc.PruneWebSessions(ctx, nil)
	e.must(err)
	if n != 2 || e.live(a) || e.live(c) {
		t.Fatalf("prune with an empty allowlist: n=%d", n)
	}
}

func TestLoginCodeRoundTrip(t *testing.T) {
	e := newEnv(t)
	code, c, err := e.svc.NewLoginCode(ctx, 7)
	e.must(err)
	if !codeRE.MatchString(code) {
		t.Fatalf("code %q has the wrong form", code)
	}
	want := model.LoginCode{CodeHash: sha(code), UserID: 7, CreatedAt: t0, ExpiresAt: t0.Add(5 * time.Minute)}
	if c != want {
		t.Fatalf("code row:\n got %+v\nwant %+v", c, want)
	}
	// The user types it with a dash, spaces and lower case.
	typed := " " + strings.ToLower(code[:4]) + "-" + strings.ToLower(code[4:]) + " "
	got, err := e.svc.RedeemLoginCode(ctx, typed)
	e.must(err)
	if got != want {
		t.Fatalf("redeem: %+v", got)
	}
	if _, err := e.svc.RedeemLoginCode(ctx, code); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second redeem: %v", err)
	}
}

func TestLoginCodeNormalisation(t *testing.T) {
	cases := map[string]string{
		"abcd-efgh":   "ABCDEFGH",
		" AB CD EF ":  "ABCDEF",
		"il1o0":       "11100",
		"IOLO-ILOL":   "10101101",
		"۱۲۳۴-abcd":   "1234ABCD", // Persian digits
		"ab\tcd\nef0": "ABCDEF0",  // every kind of space
		"ab–cd":       "AB–CD",    // only the ASCII dash
	}
	for in, want := range cases {
		if got := normLoginCode(in); got != want {
			t.Errorf("normLoginCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedeemConfusableLetters(t *testing.T) {
	e := newEnv(t)
	// Make codes until one has a 1 or a 0, then type it with I, L or O.
	for range 200 {
		code, _, err := e.svc.NewLoginCode(ctx, 7)
		e.must(err)
		if !strings.ContainsAny(code, "10") {
			continue
		}
		typed := strings.NewReplacer("1", "l", "0", "O").Replace(code)
		c, err := e.svc.RedeemLoginCode(ctx, typed)
		e.must(err)
		if c.UserID != 7 {
			t.Fatalf("redeemed %+v", c)
		}
		return
	}
	t.Fatal("no code with 1 or 0 in 200 tries")
}

func TestLoginCodeReplacesAndExpires(t *testing.T) {
	e := newEnv(t)
	first, _, err := e.svc.NewLoginCode(ctx, 7)
	e.must(err)
	second, _, err := e.svc.NewLoginCode(ctx, 7)
	e.must(err)
	if _, err := e.svc.RedeemLoginCode(ctx, first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("first code still works: %v", err)
	}
	e.clk.Advance(5 * time.Minute)
	if _, err := e.svc.RedeemLoginCode(ctx, second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("code works after 5 min: %v", err)
	}
	for _, bad := range []string{"", "ABC", "ABCDEFGHJ", "ABCDEFGU", "ABCD!FGH"} {
		if _, err := e.svc.RedeemLoginCode(ctx, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestCancelLoginCodes(t *testing.T) {
	e := newEnv(t)
	code, _, err := e.svc.NewLoginCode(ctx, 7)
	e.must(err)
	e.must(e.svc.CancelLoginCodes(ctx, 7))
	if _, err := e.svc.RedeemLoginCode(ctx, code); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelled code works: %v", err)
	}
}

func TestLoginCodeCap(t *testing.T) {
	e := newEnv(t)
	for i := range maxLiveCodes {
		if _, _, err := e.svc.NewLoginCode(ctx, int64(1000+i)); err != nil {
			t.Fatalf("code %d: %v", i, err)
		}
	}
	if _, _, err := e.svc.NewLoginCode(ctx, 5000); !errors.Is(err, ErrTooManyCodes) {
		t.Fatalf("code above the cap: %v", err)
	}
	// A user who has a live code can replace it at the cap.
	if _, _, err := e.svc.NewLoginCode(ctx, 1000); err != nil {
		t.Fatalf("replacement at the cap: %v", err)
	}
	e.clk.Advance(5 * time.Minute)
	if _, _, err := e.svc.NewLoginCode(ctx, 5000); err != nil {
		t.Fatalf("after the codes expired: %v", err)
	}
}

func TestRedeemLoginCodeTwoNodes(t *testing.T) {
	e := newEnv(t)
	_, other := e.node("B")
	code, _, err := e.svc.NewLoginCode(ctx, 7)
	e.must(err)
	var wins atomic.Int32
	run := func(s *Svc) {
		if _, err := s.RedeemLoginCode(ctx, code); err == nil {
			wins.Add(1)
		} else if !errors.Is(err, ErrNotFound) {
			t.Error(err)
		}
	}
	done := make(chan struct{})
	for i := range 6 {
		s := e.svc
		if i%2 == 1 {
			s = other
		}
		go func() { run(s); done <- struct{}{} }()
	}
	for range 6 {
		<-done
	}
	if wins.Load() != 1 {
		t.Fatalf("%d redeems won, want 1", wins.Load())
	}
}

func TestUseProofOnce(t *testing.T) {
	e := newEnv(t)
	h := sha("widget hash")
	exp := t0.Add(10 * time.Minute)
	fresh, err := e.svc.UseProof(ctx, h, exp)
	e.must(err)
	if !fresh {
		t.Fatal("first use not fresh")
	}
	e.clk.Advance(time.Minute)
	if fresh, err = e.svc.UseProof(ctx, h, exp); err != nil || fresh {
		t.Fatalf("second use: %v, %v", fresh, err)
	}
}

func TestNoteMember(t *testing.T) {
	e := newEnv(t)
	g := Actor{UserID: 7, ChatID: -100, ChatKind: model.ChatGroup, ChatTitle: "Family"}
	e.must(e.svc.NoteMember(ctx, g))
	m, err := e.svc.Membership(ctx, -100, 7)
	e.must(err)
	if m.Status != model.MemberUnknown || !m.SeenAt.Equal(t0) || !m.CheckedAt.IsZero() {
		t.Fatalf("hint row: %+v", m)
	}
	c, err := e.svc.Chat(ctx, -100)
	e.must(err)
	if c.Kind != model.ChatGroup || c.Title != "Family" {
		t.Fatalf("chat: %+v", c)
	}

	// Within 1 h nothing is written.
	e.clk.Advance(59 * time.Minute)
	e.must(e.svc.NoteMember(ctx, Actor{UserID: 7, ChatID: -100, ChatKind: model.ChatGroup, ChatTitle: "New"}))
	if m, _ := e.svc.Membership(ctx, -100, 7); !m.SeenAt.Equal(t0) {
		t.Fatalf("written within 1 h: %+v", m)
	}
	// After 1 h the hint moves seen_at, the title follows, and the status stays.
	e.must(e.svc.RecordMembership(ctx, model.ChatMember{ChatID: -100, UserID: 7, Status: model.MemberYes}))
	e.clk.Advance(time.Minute)
	e.must(e.svc.NoteMember(ctx, Actor{UserID: 7, ChatID: -100, ChatKind: model.ChatGroup, ChatTitle: "New"}))
	m, err = e.svc.Membership(ctx, -100, 7)
	e.must(err)
	if m.Status != model.MemberYes || !m.SeenAt.Equal(e.clk.Now()) || !m.CheckedAt.Equal(t0.Add(59*time.Minute)) {
		t.Fatalf("after 1 h: %+v", m)
	}
	if c, _ := e.svc.Chat(ctx, -100); c.Title != "New" {
		t.Fatalf("title: %q", c.Title)
	}

	// A private chat is not a group: no row.
	e.must(e.svc.NoteMember(ctx, alice))
	if _, err := e.svc.Membership(ctx, alice.ChatID, alice.UserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private chat got a row: %v", err)
	}
	// An empty title never erases the stored one.
	e.clk.Advance(2 * time.Hour)
	e.must(e.svc.NoteMember(ctx, Actor{UserID: 8, ChatID: -100, ChatKind: model.ChatGroup}))
	if c, _ := e.svc.Chat(ctx, -100); c.Title != "New" {
		t.Fatalf("empty title erased the name: %q", c.Title)
	}
}

func TestMembershipReadsAndChatGone(t *testing.T) {
	e := newEnv(t)
	for _, chat := range []int64{-3, -1, -2} {
		e.must(e.svc.RecordMembership(ctx, model.ChatMember{ChatID: chat, UserID: 7, Status: model.MemberYes, CheckedAt: t0}))
	}
	e.must(e.svc.RecordMembership(ctx, model.ChatMember{ChatID: -1, UserID: 8, Status: model.MemberYes, CheckedAt: t0}))
	ms, err := e.svc.MembershipsOf(ctx, 7)
	e.must(err)
	if len(ms) != 3 || ms[0].ChatID != -3 || ms[2].ChatID != -1 {
		t.Fatalf("memberships: %+v", ms)
	}
	e.clk.Advance(time.Minute)
	e.must(e.svc.ChatGone(ctx, -1))
	for _, u := range []int64{7, 8} {
		m, err := e.svc.Membership(ctx, -1, u)
		e.must(err)
		if m.Status != model.MemberLeft || !m.CheckedAt.Equal(e.clk.Now()) {
			t.Fatalf("user %d after gone: %+v", u, m)
		}
	}
	if m, _ := e.svc.Membership(ctx, -2, 7); m.Status != model.MemberYes {
		t.Fatalf("gone touched another chat: %+v", m)
	}
}

func TestRecordMembershipDefaultsCheckedAt(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.RecordMembership(ctx, model.ChatMember{ChatID: -1, UserID: 7, Status: model.MemberLeft}))
	m, err := e.svc.Membership(ctx, -1, 7)
	e.must(err)
	if !m.CheckedAt.Equal(t0) {
		t.Fatalf("checked_at = %v, want now", m.CheckedAt)
	}
}

func TestGroupChatsChatAndUser(t *testing.T) {
	e := newEnv(t)
	e.add(Actor{UserID: 7, ChatID: -20, ChatKind: model.ChatGroup, ChatTitle: "b"}, "x", nil, nil)
	e.add(Actor{UserID: 7, ChatID: -10, ChatKind: model.ChatGroup, ChatTitle: "a"}, "x", nil, nil)
	e.add(alice, "x", nil, nil)
	cs, err := e.svc.GroupChats(ctx, 50)
	e.must(err)
	if len(cs) != 2 || cs[0].ID != -20 || cs[1].ID != -10 {
		t.Fatalf("groups: %+v", cs)
	}
	if _, err := e.svc.Chat(ctx, -999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing chat: %v", err)
	}
	u, err := e.svc.User(ctx, 7)
	e.must(err)
	if u.ID != 7 {
		t.Fatalf("user: %+v", u)
	}
	if _, err := e.svc.User(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
}

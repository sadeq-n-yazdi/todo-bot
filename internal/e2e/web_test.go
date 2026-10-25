package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/config"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgbot"
	"github.com/sadeq-n-yazdi/todo-bot/internal/web"
)

const (
	webToken  = "4242:web-e2e-token"
	webHost   = "todo.test"
	webOrigin = "https://" + webHost
	memberTTL = 10 * time.Minute
)

// webNode is one bot process with the web UI on, wired like runBot and
// startWeb in cmd/todo-bot/run.go: the stack sends through the real tgbot
// adapter to the mock Bot API, the handler is wrapped in web.Observe, and
// the web server listens on 127.0.0.1:0. It has no lease loop and no
// poller: the test delivers updates to the observed handler and drives the
// scheduler with tick, because the fake clock jumps by more than a day.
type webNode struct {
	*stack
	handle tg.Handler
	base   string // http://127.0.0.1:port
	http   *http.Client
}

func startWebNode(t *testing.T, m *mockTelegram, clk *clocktest.Fake) *webNode {
	t.Helper()
	bot, err := tgbot.New(context.Background(), webToken, tgbot.Options{ServerURL: m.srv.URL})
	if err != nil {
		t.Fatalf("tgbot.New: %v", err)
	}
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{sender: bot})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		WebAddr:       "127.0.0.1:0",
		WebOrigin:     webOrigin,
		WebSessionTTL: 720 * time.Hour,
		WebSessionMax: 2160 * time.Hour,
		WebMemberTTL:  memberTTL,
		WebTrustedProxies: []netip.Prefix{
			netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128"),
		},
	}
	srv := web.New(web.Deps{
		Svc: s.svc, Web: s.svc, Send: bot, Members: bot,
		Parser: nlparse.Parser{DefaultHour: 9}, Clock: clk,
		Allowed: allowedUsers(), Token: webToken, BotName: bot.Username(),
		Config: cfg, Assets: web.Assets(), Log: log,
	})
	l, err := net.Listen("tcp", cfg.WebAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(l); err != nil {
			t.Errorf("web serve: %v", err)
		}
	}()
	// Registered after newStack, so it runs first: the web stops before
	// the database closes.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("web shutdown: %v", err)
		}
		<-done
	})
	return &webNode{
		stack:  s,
		handle: web.Observe(s.h.Handle, s.svc, allowedUsers(), log),
		base:   "http://" + l.Addr().String(),
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

// sayIn delivers a text message from user (with a @username) in chat
// through the observed handler, as the poller of run.go does.
func (n *webNode) sayIn(from int64, username string, chat int64, text string) {
	n.t.Helper()
	kind, title := model.ChatPrivate, ""
	if chat < 0 {
		kind, title = model.ChatGroup, "Family"
	}
	id := updateSeq.Add(1)
	u := tg.Update{ID: id, Message: &tg.Message{
		ID: int(id), ChatID: chat, ChatKind: kind, ChatTitle: title,
		FromID: from, FromName: "u", FromUsername: username, Text: text,
	}}
	if err := n.handle(context.Background(), u); err != nil {
		n.t.Fatalf("handle update %d: %v", id, err)
	}
}

// cred is how a client proves its session: a bearer token (Mini App) or a
// cookie (browser). The zero value sends neither.
type cred struct{ bearer, cookie string }

type webResp struct {
	code   int
	body   []byte
	header http.Header
}

// call sends one request to the web server, with the Host of the origin. A
// write gets the CSRF headers of the app (WEB-DESIGN §3.6).
func (n *webNode) call(c cred, method, path string, body any) webResp {
	n.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			n.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, n.base+path, rd)
	if err != nil {
		n.t.Fatal(err)
	}
	req.Host = webHost
	if method != http.MethodGet {
		req.Header.Set("X-Todo-Request", "1")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", webOrigin)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	if c.cookie != "" {
		// Set by hand: the cookie is Secure, and an http.CookieJar does not
		// send it over plain HTTP.
		req.Header.Set("Cookie", c.cookie)
	}
	res, err := n.http.Do(req)
	if err != nil {
		n.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		n.t.Fatal(err)
	}
	return webResp{code: res.StatusCode, body: b, header: res.Header}
}

// must sends a request, checks the status and decodes the JSON answer into
// out (if not nil).
func (n *webNode) must(c cred, want int, method, path string, body, out any) webResp {
	n.t.Helper()
	r := n.call(c, method, path, body)
	if r.code != want {
		n.t.Fatalf("%s %s: status %d, want %d: %s", method, path, r.code, want, r.body)
	}
	if out != nil {
		if err := json.Unmarshal(r.body, out); err != nil {
			n.t.Fatalf("%s %s: decode %s: %v", method, path, r.body, err)
		}
	}
	return r
}

// initData signs Mini App launch data for user with the steps of
// WEB-DESIGN §3.2, dated at the current fake time.
func (n *webNode) initData(user int64, username string) string {
	u, _ := json.Marshal(map[string]any{"id": user, "first_name": "U", "username": username, "language_code": "en"})
	fields := map[string]string{
		"auth_date": strconv.FormatInt(n.clk.Now().Unix(), 10),
		"query_id":  "AAH-e2e",
		"user":      string(u),
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(webToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return v.Encode()
}

// miniApp signs user in as the Mini App does and returns the bearer token.
func (n *webNode) miniApp(user int64, username string) cred {
	n.t.Helper()
	var out struct {
		Token string `json:"token"`
		User  struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	n.must(cred{}, http.StatusOK, "POST", "/api/auth/miniapp", map[string]string{"init_data": n.initData(user, username)}, &out)
	if out.Token == "" || out.User.ID != user {
		n.t.Fatalf("miniapp sign-in: %+v", out)
	}
	return cred{bearer: out.Token}
}

type webItem struct {
	ID        int64      `json:"id"`
	ChatID    int64      `json:"chat_id"`
	Text      string     `json:"text"`
	DueAt     *time.Time `json:"due_at"`
	Recur     string     `json:"recur"`
	CreatedBy int64      `json:"created_by"`
	Assignee  *struct {
		ID int64 `json:"id"`
	} `json:"assignee"`
}

type webList struct {
	Items []webItem `json:"items"`
}

func findItem(items []webItem, id int64) (webItem, bool) {
	for _, it := range items {
		if it.ID == id {
			return it, true
		}
	}
	return webItem{}, false
}

// The e2e test of WEB-DESIGN §12.3: the full process with the web on, the
// mock Bot API and the fake clock.
func TestWebEndToEnd(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0)) // Saturday
	m := newMockTelegram(t)
	n := startWebNode(t, m, clk)
	ctx := context.Background()

	// 1. Sign in with a signed initData.
	app := n.miniApp(alice, "alice")
	var me struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
		Session struct {
			Kind string `json:"kind"`
		} `json:"session"`
	}
	n.must(app, http.StatusOK, "GET", "/api/me", nil, &me)
	if me.User.ID != alice || me.Session.Kind != "miniapp" {
		t.Fatalf("me %+v", me)
	}

	// 2. A login code from the Mini App, redeemed by a client with no
	// session. The client gets the cookie, and the sign-in notice arrives
	// one time.
	var code struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	n.must(app, http.StatusCreated, "POST", "/api/auth/code", map[string]string{"init_data": n.initData(alice, "alice")}, &code)
	if len(code.Code) != 9 || code.Code[4] != '-' {
		t.Fatalf("code %q", code.Code)
	}
	if want := clk.Now().Add(5 * time.Minute); !code.ExpiresAt.Equal(want) {
		t.Errorf("code expires at %v, want %v", code.ExpiresAt, want)
	}
	r := n.must(cred{}, http.StatusOK, "POST", "/api/auth/code/redeem", map[string]string{"code": code.Code}, nil)
	var browser cred
	for _, c := range (&http.Response{Header: r.header}).Cookies() {
		if c.Name == "__Host-tb_session" && c.Value != "" {
			if !c.Secure || !c.HttpOnly {
				t.Errorf("cookie %+v is not Secure and HttpOnly", c)
			}
			browser = cred{cookie: c.Name + "=" + c.Value}
		}
	}
	if browser.cookie == "" {
		t.Fatalf("redeem set no session cookie: %v", r.header["Set-Cookie"])
	}
	n.must(browser, http.StatusOK, "GET", "/api/me", nil, &me)
	if me.User.ID != alice || me.Session.Kind != "browser" {
		t.Fatalf("browser me %+v", me)
	}
	// The code works one time.
	if r := n.call(cred{}, "POST", "/api/auth/code/redeem", map[string]string{"code": code.Code}); r.code != http.StatusUnauthorized {
		t.Errorf("second redeem: %d %s", r.code, r.body)
	}
	isSignIn := func(c tgCall) bool {
		return c.Method == "sendMessage" && c.ChatID == alice && strings.Contains(c.Text, "New sign-in")
	}
	waitUntil(t, "the sign-in notice", func() bool { return m.count(isSignIn) > 0 })
	if got := m.count(isSignIn); got != 1 {
		t.Errorf("%d sign-in notices, want 1", got)
	}

	// 3. Add through the API. The stored row is the row that /add gives
	// with the same words in a second chat.
	const words = "buy milk tomorrow 18:00 every week"
	var added webItem
	n.must(browser, http.StatusCreated, "POST", fmt.Sprintf("/api/chats/%d/items", alice), map[string]any{"text": words}, &added)
	n.sayIn(bob, "bob", bob, "/add "+words)
	webRow, err := n.st.GetTodo(ctx, added.ID)
	if err != nil {
		t.Fatal(err)
	}
	botRow := n.todoByNum(bob, 1)
	if webRow.Text != "buy milk" || webRow.Text != botRow.Text || webRow.Recur != botRow.Recur || webRow.TZ != botRow.TZ ||
		webRow.DueAt == nil || botRow.DueAt == nil || !webRow.DueAt.Equal(*botRow.DueAt) {
		t.Fatalf("web row %+v\n/add row %+v", webRow, botRow)
	}
	due := utc(2026, 10, 11, 18, 0)
	if !webRow.DueAt.Equal(due) || webRow.Recur == "" {
		t.Fatalf("web row due %v recur %q", webRow.DueAt, webRow.Recur)
	}

	// 4. Read the list.
	var mine webList
	n.must(browser, http.StatusOK, "GET", fmt.Sprintf("/api/chats/%d/items", alice), nil, &mine)
	if it, ok := findItem(mine.Items, added.ID); !ok || it.Text != "buy milk" || it.DueAt == nil || !it.DueAt.Equal(due) {
		t.Fatalf("list %+v", mine.Items)
	}

	// 5. The reminder arrives one time.
	isReminder := func(c tgCall) bool {
		return c.Method == "sendMessage" && c.ChatID == alice && strings.HasPrefix(c.Text, "⏰ ")
	}
	n.advanceTo(due)
	n.tick()
	if got := m.count(isReminder); got != 1 {
		t.Fatalf("%d reminders to alice, want 1", got)
	}

	// 6. Done through the API. The item repeats, so it stays open with the
	// next date.
	var done struct {
		Item      webItem `json:"item"`
		StillOpen bool    `json:"still_open"`
	}
	n.must(browser, http.StatusOK, "POST", fmt.Sprintf("/api/chats/%d/items/%d/done", alice, added.ID), map[string]any{}, &done)
	if !done.StillOpen || done.Item.DueAt == nil || !done.Item.DueAt.Equal(due.AddDate(0, 0, 7)) {
		t.Fatalf("done %+v", done)
	}

	// 7. A group: getChatMember answers member. Alice lists the group and
	// adds an item for bob. Bob writes in the group, so the bot knows his
	// @username.
	n.sayIn(alice, "alice", group, "/list")
	n.sayIn(bob, "bob", group, "/list")
	m.setMember(group, alice, "member")
	var chats struct {
		Chats []struct {
			ID   int64  `json:"id"`
			Kind string `json:"kind"`
		} `json:"chats"`
		Partial bool `json:"partial"`
	}
	n.must(browser, http.StatusOK, "GET", "/api/chats", nil, &chats)
	if len(chats.Chats) != 2 || chats.Chats[0].ID != alice || chats.Chats[1].ID != group || chats.Chats[1].Kind != "group" || chats.Partial {
		t.Fatalf("chats %+v", chats)
	}
	groupItems := fmt.Sprintf("/api/chats/%d/items", group)
	n.must(browser, http.StatusOK, "GET", groupItems, nil, nil)
	var bins webItem
	n.must(browser, http.StatusCreated, "POST", groupItems, map[string]any{"text": "take out the bins", "assignee_id": bob}, &bins)
	if bins.ChatID != group || bins.Assignee == nil || bins.Assignee.ID != bob {
		t.Fatalf("group item %+v", bins)
	}
	var list webList
	n.must(browser, http.StatusOK, "GET", groupItems, nil, &list)
	if _, ok := findItem(list.Items, bins.ID); !ok {
		t.Fatalf("group list %+v", list.Items)
	}

	// 8. Bob completes it from Assigned to me, from his own chat. The "did"
	// notice arrives in the group one time.
	bobApp := n.miniApp(bob, "bob")
	var assigned webList
	n.must(bobApp, http.StatusOK, "GET", "/api/assigned", nil, &assigned)
	if _, ok := findItem(assigned.Items, bins.ID); !ok {
		t.Fatalf("assigned %+v", assigned.Items)
	}
	n.must(bobApp, http.StatusOK, "POST", fmt.Sprintf("/api/chats/%d/items/%d/done", bob, bins.ID), map[string]any{}, &done)
	if done.StillOpen {
		t.Fatalf("done %+v", done)
	}
	isDid := func(c tgCall) bool {
		return c.Method == "sendMessage" && c.ChatID == group && strings.Contains(c.Text, " did: take out the bins")
	}
	waitUntil(t, "the did notice", func() bool { return m.count(isDid) > 0 })
	if got := m.count(isDid); got != 1 {
		t.Errorf("%d did notices, want 1", got)
	}

	// 9. Alice leaves the group. After TODO_WEB_MEMBER_TTL, the group list
	// gives 403.
	m.setMember(group, alice, "left")
	isCheck := func(c tgCall) bool {
		return c.Method == "getChatMember" && c.ChatID == group && c.UserID == alice
	}
	checks := m.count(isCheck)
	n.must(browser, http.StatusOK, "GET", groupItems, nil, nil) // the member answer is still fresh
	if got := m.count(isCheck); got != checks {
		t.Errorf("a fresh member row called getChatMember")
	}
	clk.Advance(memberTTL + time.Second)
	var fail struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	n.must(browser, http.StatusForbidden, "GET", groupItems, nil, &fail)
	if fail.Error.Code != "not_member" {
		t.Errorf("error %s, want not_member", fail.Error.Code)
	}
	if got := m.count(isCheck); got != checks+1 {
		t.Errorf("%d getChatMember calls after the TTL, want 1", got-checks)
	}

	// No reminder or notice was sent twice.
	if got := m.count(isReminder); got != 1 {
		t.Errorf("%d reminders to alice at the end, want 1", got)
	}
}

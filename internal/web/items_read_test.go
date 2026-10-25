package web

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/api")

const (
	groupFamily = -1001234567890
	groupNoRow  = -1009999999999 // a group with an empty title
)

// setUser sets the language, calendar and time zone of a user.
func (h *harness) setUser(id int64, lang i18n.Lang, cal calendar.Kind, tz string) {
	h.t.Helper()
	ctx := context.Background()
	if err := h.st.UpsertUser(ctx, model.User{ID: id, TZ: tz, Lang: lang, Calendar: cal, CreatedAt: testNow}); err != nil {
		h.t.Fatal(err)
	}
	if err := h.st.EnsureChat(ctx, model.Chat{ID: id, Kind: model.ChatPrivate, CreatedAt: testNow}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) addTodo(t model.Todo) model.Todo {
	h.t.Helper()
	if t.Status == "" {
		t.Status = model.TodoOpen
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt, t.UpdatedAt = testNow, testNow
	}
	if err := h.st.InsertTodo(context.Background(), &t); err != nil {
		h.t.Fatal(err)
	}
	return t
}

func at(y int, m time.Month, d, hh, mm int) *time.Time {
	t := time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
	return &t
}

// seedFixture fills the private chat of Ann, the group "Family" and the
// items that other chats assigned to Ann. testNow is Sunday 1 Mar 2026
// 12:00 UTC; Ann's time zone is Asia/Tehran (UTC+3:30 all year).
func (h *harness) seedFixture() {
	h.t.Helper()
	ctx := context.Background()
	for _, c := range []model.Chat{
		{ID: idAnn, Kind: model.ChatPrivate, Title: "Ann", CreatedAt: testNow},
		{ID: groupFamily, Kind: model.ChatGroup, Title: "Family", CreatedAt: testNow},
		{ID: groupNoRow, Kind: model.ChatGroup, CreatedAt: testNow}, // no title
	} {
		if err := h.st.EnsureChat(ctx, c); err != nil {
			h.t.Fatal(err)
		}
	}
	if err := h.st.UpsertUser(ctx, model.User{ID: idBob, TZ: "UTC", Lang: i18n.En, Calendar: calendar.Gregorian, CreatedAt: testNow}); err != nil {
		h.t.Fatal(err)
	}
	if err := h.st.SetUsername(ctx, idBob, "bob"); err != nil {
		h.t.Fatal(err)
	}
	// 777 is a user whose @username the bot does not know.
	if err := h.st.UpsertUser(ctx, model.User{ID: 777, TZ: "UTC", Lang: i18n.En, Calendar: calendar.Gregorian, CreatedAt: testNow}); err != nil {
		h.t.Fatal(err)
	}
	// The private chat of Ann: a date for tomorrow, no date, overdue and
	// weekly, a date in another year, and two assigned items.
	h.addTodo(model.Todo{ChatID: idAnn, Text: "buy milk", DueAt: at(2026, 3, 2, 14, 30), CreatedBy: idAnn})
	h.addTodo(model.Todo{ChatID: idAnn, Text: "call mom", CreatedBy: idAnn})
	h.addTodo(model.Todo{ChatID: idAnn, Text: "water the plants", DueAt: at(2026, 2, 22, 14, 30),
		Recur: "weekly sun 18:00", TZ: "Asia/Tehran", CreatedBy: idAnn})
	h.addTodo(model.Todo{ChatID: idAnn, Text: "renew passport", DueAt: at(2027, 5, 10, 6, 0), CreatedBy: idAnn})
	h.addTodo(model.Todo{ChatID: idAnn, Text: "send the report", DueAt: at(2026, 3, 4, 9, 0), AssigneeID: idBob, CreatedBy: idAnn})
	h.addTodo(model.Todo{ChatID: idAnn, Text: "ask the neighbour", AssigneeID: 777, CreatedBy: idAnn}) // no username known
	// A group item that repeats each month on the 1st, in the Jalali calendar.
	h.addTodo(model.Todo{ChatID: groupFamily, Text: "pay the rent", DueAt: at(2026, 3, 21, 8, 30),
		Recur: "monthly 1 08:00 jalali", TZ: "Asia/Tehran", CreatedBy: idBob})
	// Assigned to Ann: from the group, from a group with no title, and one
	// that is done (not listed).
	h.addTodo(model.Todo{ChatID: groupFamily, Text: "book the flight", DueAt: at(2026, 3, 9, 7, 0), AssigneeID: idAnn, CreatedBy: idBob})
	h.addTodo(model.Todo{ChatID: groupNoRow, Text: "bring the cake", AssigneeID: idAnn, CreatedBy: idBob})
}

// callAs calls a handler of the list routes for the user with an actor,
// without the route table. It lets a test use a group actor, which the
// router gives only after W4.
func (h *harness) callAs(user int64, a service.Actor, path string, opts ...reqOpt) *httptest.ResponseRecorder {
	h.t.Helper()
	prefs, err := h.svc.Prefs(context.Background(), user, "")
	if err != nil {
		h.t.Fatal(err)
	}
	r := httptest.NewRequest("GET", path, nil)
	for _, o := range opts {
		o(r)
	}
	ri := &reqInfo{loc: prefs.Locale, sess: &session{WebSession: model.WebSession{UserID: user}, prefs: prefs}}
	ctx := context.WithValue(r.Context(), reqInfoKey{}, ri)
	ctx = context.WithValue(ctx, actorKey{}, a)
	w := httptest.NewRecorder()
	h.srv.handleListItems(w, r.WithContext(ctx))
	return w
}

func pretty(t *testing.T, raw []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := json.Indent(&b, bytes.TrimSpace(raw), "", "  "); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	b.WriteByte('\n')
	return b.Bytes()
}

// golden compares the body with testdata/api/<name>.json, or rewrites it
// with -update.
func golden(t *testing.T, name string, body []byte) {
	t.Helper()
	path := filepath.Join("testdata", "api", name+".json")
	got := pretty(t, body)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/web -run Golden -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file:\n--- got\n%s--- want\n%s", name, got, want)
	}
}

func TestGoldenLists(t *testing.T) {
	cases := []struct {
		name string
		lang i18n.Lang
		cal  calendar.Kind
	}{
		{"en_gregorian", i18n.En, calendar.Gregorian},
		{"en_jalali", i18n.En, calendar.Jalali},
		{"fa_gregorian", i18n.Fa, calendar.Gregorian},
		{"fa_jalali", i18n.Fa, calendar.Jalali},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.setUser(idAnn, c.lang, c.cal, "Asia/Tehran")
			h.seedFixture()
			tok, _ := h.browser(idAnn)

			w := h.do("GET", fmt.Sprintf("/api/chats/%d/items", idAnn), "", cookie(tok))
			if w.Code != 200 {
				t.Fatalf("list: %d %s", w.Code, w.Body)
			}
			golden(t, "list_mine_"+c.name, w.Body.Bytes())

			w = h.do("GET", "/api/assigned", "", cookie(tok))
			if w.Code != 200 {
				t.Fatalf("assigned: %d %s", w.Code, w.Body)
			}
			golden(t, "assigned_"+c.name, w.Body.Bytes())

			// A group list, through a group actor.
			a := service.Actor{UserID: idAnn, ChatID: groupFamily, ChatKind: model.ChatGroup, ChatTitle: "Family"}
			w = h.callAs(idAnn, a, "/api/chats/x/items")
			if w.Code != 200 {
				t.Fatalf("group: %d %s", w.Code, w.Body)
			}
			golden(t, "list_group_"+c.name, w.Body.Bytes())
		})
	}
}

func TestGoldenEmptyAndPaged(t *testing.T) {
	h := newHarness(t)
	h.setUser(idAnn, i18n.Fa, calendar.Jalali, "Asia/Tehran")
	tok, _ := h.browser(idAnn)
	path := fmt.Sprintf("/api/chats/%d/items", idAnn)
	w := h.do("GET", path, "", cookie(tok))
	golden(t, "list_empty", w.Body.Bytes())
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Errorf("an empty list must be [], not null: %s", w.Body)
	}

	for i := 1; i <= 12; i++ {
		h.addTodo(model.Todo{ChatID: idAnn, Text: fmt.Sprintf("item %d", i), CreatedBy: idAnn})
	}
	w = h.do("GET", path+"?page=1", "", cookie(tok))
	golden(t, "list_page2", w.Body.Bytes())
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) listDTO {
	t.Helper()
	var l listDTO
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestListPaging(t *testing.T) {
	h := newHarness(t)
	h.setUser(idAnn, i18n.En, calendar.Gregorian, "UTC")
	for i := 1; i <= 25; i++ {
		h.addTodo(model.Todo{ChatID: idAnn, Text: fmt.Sprintf("item %d", i), CreatedBy: idAnn})
	}
	tok, _ := h.browser(idAnn)
	path := fmt.Sprintf("/api/chats/%d/items", idAnn)
	for _, c := range []struct {
		q          string
		page, n    int
		wantStatus int
	}{
		{"", 0, 10, 200}, {"?page=2", 2, 5, 200}, {"?page=99", 2, 5, 200},
		{"?page=-1", 0, 0, 400}, {"?page=x", 0, 0, 400},
	} {
		w := h.do("GET", path+c.q, "", cookie(tok))
		if w.Code != c.wantStatus {
			t.Fatalf("%q: status %d, want %d: %s", c.q, w.Code, c.wantStatus, w.Body)
		}
		if c.wantStatus != 200 {
			wantErr(t, w, 400, "bad_request")
			continue
		}
		l := decodeList(t, w)
		if l.Page != c.page || l.Pages != 3 || len(l.Items) != c.n {
			t.Errorf("%q: page %d of %d with %d items", c.q, l.Page, l.Pages, len(l.Items))
		}
	}
}

func TestListRefusals(t *testing.T) {
	h := newHarness(t)
	h.setUser(idAnn, i18n.En, calendar.Gregorian, "UTC")
	h.setUser(idBob, i18n.En, calendar.Gregorian, "UTC")
	h.seedFixture()
	tok, _ := h.browser(idBob)
	wantErr(t, h.do("GET", "/api/chats/"+fmt.Sprint(idAnn)+"/items", "", cookie(tok)), 404, "not_found") // another private chat
	// A group the user is not a member of: 403 or 404 (the actor middleware).
	w := h.do("GET", fmt.Sprintf("/api/chats/%d/items", groupFamily), "", cookie(tok))
	if w.Code != 403 && w.Code != 404 {
		t.Errorf("group: status %d, want 403 or 404: %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "buy milk") || strings.Contains(w.Body.String(), "pay the rent") {
		t.Errorf("a refusal leaks items: %s", w.Body)
	}
	wantErr(t, h.do("GET", "/api/chats/"+fmt.Sprint(idBob)+"/items", ""), 401, "unauthenticated")
	wantErr(t, h.do("GET", "/api/assigned", ""), 401, "unauthenticated")
}

// The handler uses the chat of the actor and never {chat}.
func TestListUsesActorNotPath(t *testing.T) {
	h := newHarness(t)
	h.setUser(idAnn, i18n.En, calendar.Gregorian, "UTC")
	h.seedFixture()
	a := service.Actor{UserID: idAnn, ChatID: groupFamily, ChatKind: model.ChatGroup}
	l := decodeList(t, h.callAs(idAnn, a, fmt.Sprintf("/api/chats/%d/items", idAnn)))
	if len(l.Items) != 2 { // "pay the rent" and "book the flight"
		t.Fatalf("items %+v", l.Items)
	}
	for _, it := range l.Items {
		if it.ChatID != groupFamily || it.ChatTitle != "" {
			t.Errorf("item %+v: want the group chat and no chat_title", it)
		}
	}
}

func TestETag(t *testing.T) {
	h := newHarness(t)
	h.setUser(idAnn, i18n.En, calendar.Gregorian, "UTC")
	h.seedFixture()
	tok, _ := h.browser(idAnn)
	for _, path := range []string{fmt.Sprintf("/api/chats/%d/items", idAnn), "/api/assigned"} {
		w1 := h.do("GET", path, "", cookie(tok))
		tag := w1.Header().Get("ETag")
		if !strings.HasPrefix(tag, `W/"`) || len(tag) != len(`W/""`)+16 {
			t.Fatalf("%s: ETag %q", path, tag)
		}
		hd := w1.Header()
		if hd.Get("Cache-Control") != "private, no-cache" || hd.Get("Vary") != "Authorization, Cookie" {
			t.Errorf("%s: headers %v", path, hd)
		}
		// rendered_at moves, the ETag stays, and the tag gives 304.
		h.clk.Advance(0) // same instant is not the test: advance a little
		h.clk.Advance(time.Second)
		w2 := h.do("GET", path, "", cookie(tok), header("If-None-Match", tag))
		if w2.Code != http.StatusNotModified || w2.Body.Len() != 0 {
			t.Fatalf("%s: status %d, body %q", path, w2.Code, w2.Body)
		}
		if w2.Header().Get("ETag") != tag || w2.Header().Get("Cache-Control") != "private, no-cache" {
			t.Errorf("%s: 304 headers %v", path, w2.Header())
		}
		w3 := h.do("GET", path, "", cookie(tok))
		if w3.Code != 200 || w3.Header().Get("ETag") != tag {
			t.Fatalf("%s: second 200 has ETag %q, want %q", path, w3.Header().Get("ETag"), tag)
		}
		if decodeList(t, w3).RenderedAt.Equal(decodeList(t, w1).RenderedAt) {
			t.Errorf("%s: rendered_at did not change; the test proves nothing", path)
		}
		// A list of several tags, a strong form and a wrong tag.
		if w := h.do("GET", path, "", cookie(tok), header("If-None-Match", `"x", `+tag)); w.Code != 304 {
			t.Errorf("%s: list of tags gave %d", path, w.Code)
		}
		if w := h.do("GET", path, "", cookie(tok), header("If-None-Match", `W/"0000000000000000"`)); w.Code != 200 {
			t.Errorf("%s: wrong tag gave %d", path, w.Code)
		}
	}
	// A change of the data changes the tag.
	path := fmt.Sprintf("/api/chats/%d/items", idAnn)
	before := h.do("GET", path, "", cookie(tok)).Header().Get("ETag")
	h.addTodo(model.Todo{ChatID: idAnn, Text: "new", CreatedBy: idAnn})
	if after := h.do("GET", path, "", cookie(tok)).Header().Get("ETag"); after == before {
		t.Error("ETag did not change with the items")
	}
}

func TestAssigned(t *testing.T) {
	h := newHarness(t)
	h.setUser(idAnn, i18n.En, calendar.Gregorian, "UTC")
	h.seedFixture()
	tok, _ := h.browser(idAnn)
	l := decodeList(t, h.do("GET", "/api/assigned", "", cookie(tok)))
	if l.Page != 0 || l.Pages != 1 || len(l.Items) != 2 {
		t.Fatalf("assigned %+v", l)
	}
	titles := map[string]string{}
	for _, it := range l.Items {
		titles[it.Text] = it.ChatTitle
		if it.Assignee == nil || it.Assignee.ID != idAnn {
			t.Errorf("assignee of %q: %+v", it.Text, it.Assignee)
		}
	}
	if titles["book the flight"] != "Family" || titles["bring the cake"] != "" {
		t.Errorf("chat titles %v", titles)
	}
	// Only Assigned to me fills chat_title.
	for _, it := range decodeList(t, h.do("GET", fmt.Sprintf("/api/chats/%d/items", idAnn), "", cookie(tok))).Items {
		if it.ChatTitle != "" {
			t.Errorf("mine item %q has chat_title %q", it.Text, it.ChatTitle)
		}
	}
}

func TestAssignedCap(t *testing.T) {
	h := newHarness(t)
	h.setUser(idAnn, i18n.En, calendar.Gregorian, "UTC")
	if err := h.st.EnsureChat(context.Background(), model.Chat{ID: groupFamily, Kind: model.ChatGroup, Title: "Family", CreatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxAssigned+15; i++ {
		h.addTodo(model.Todo{ChatID: groupFamily, Text: fmt.Sprintf("job %d", i), AssigneeID: idAnn, CreatedBy: idBob})
	}
	tok, _ := h.browser(idAnn)
	l := decodeList(t, h.do("GET", "/api/assigned", "", cookie(tok)))
	if len(l.Items) != 200 || l.Pages != 1 {
		t.Errorf("got %d items on %d pages, want 200 on 1", len(l.Items), l.Pages)
	}
}

func TestFormatFull(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-10-25 14:30 UTC is Sunday 25 Oct 18:00 in Tehran, 3 Aban 1405.
	ts := time.Date(2026, 10, 25, 14, 30, 0, 0, time.UTC)
	for _, c := range []struct {
		lc   i18n.Locale
		want string
	}{
		{i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}, "Sun 25 Oct 2026 18:00"},
		{i18n.Locale{Lang: i18n.En, Cal: calendar.Jalali}, "Sun 3 Aban 1405 18:00"},
		{i18n.Locale{Lang: i18n.Fa, Cal: calendar.Gregorian}, "یکشنبه ۲۵ اکتبر ۲۰۲۶ ۱۸:۰۰"},
		{i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}, "یکشنبه ۳ آبان ۱۴۰۵ ۱۸:۰۰"},
	} {
		if got := formatFull(ts, c.lc, loc); got != c.want {
			t.Errorf("%+v: %q, want %q", c.lc, got, c.want)
		}
	}
	// A nil zone is UTC; an invalid calendar is Gregorian.
	if got := formatFull(ts, i18n.Locale{Lang: i18n.En}, nil); got != "Sun 25 Oct 2026 14:30" {
		t.Errorf("nil zone: %q", got)
	}
	// The date follows the zone, not UTC: 22:00 UTC is already the next day.
	late := time.Date(2026, 10, 25, 22, 0, 0, 0, time.UTC)
	if got := formatFull(late, i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}, loc); got != "Mon 26 Oct 2026 01:30" {
		t.Errorf("zone: %q", got)
	}
}

func TestItemDTO(t *testing.T) {
	v := view{lc: i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}, loc: time.UTC, now: testNow}
	none := v.toItem(model.Todo{ID: 1, Text: "x", Status: model.TodoOpen}, "")
	b, _ := json.Marshal(none)
	for _, k := range []string{`"due_at":null`, `"due_text":null`, `"due_full":null`, `"assignee":null`, `"recur_text":""`, `"overdue":false`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("%s missing in %s", k, b)
		}
	}
	over := v.toItem(model.Todo{Status: model.TodoOpen, DueAt: at(2026, 3, 1, 11, 59)}, "")
	if !over.Overdue {
		t.Error("a past date on an open item must be overdue")
	}
	if v.toItem(model.Todo{Status: model.TodoOpen, DueAt: at(2026, 3, 1, 12, 1)}, "").Overdue {
		t.Error("a future date must not be overdue")
	}
}

func TestItemStringsRegistered(t *testing.T) {
	for _, l := range []i18n.Lang{i18n.En, i18n.Fa} {
		lc := i18n.Locale{Lang: l}
		for _, k := range []string{"web.item.noname", "web.item.overdue"} {
			if m := lc.T(k); m == "" || m == k {
				t.Errorf("%s/%s not registered: %q", l, k, m)
			}
		}
	}
}

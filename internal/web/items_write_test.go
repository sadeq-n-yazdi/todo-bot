package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// writeEnv is a harness for the write API: the fixture of items_read_test,
// Ann (fa, Gregorian, Asia/Tehran) and Bob (en, UTC) signed in, Ann a
// member of the group Family, and TODO_DEFAULT_TIME 08:30.
type writeEnv struct {
	*harness
	ann, bob string // tokens
}

func newWriteEnv(t *testing.T) *writeEnv {
	t.Helper()
	h := newHarness(t)
	t.Cleanup(h.srv.codes.notices.Wait) // the "did" notices in flight
	h.srv.d.Config.DefaultHour, h.srv.d.Config.DefaultMinute = 8, 30
	h.setUser(idAnn, i18n.Fa, calendar.Gregorian, "Asia/Tehran")
	h.seedFixture()
	e := &writeEnv{harness: h}
	e.ann = h.signIn(userAnn)
	e.bob = h.signIn(userBob)
	h.members.Set(groupFamily, idAnn, tg.ChatMember{Status: tg.MemberMember})
	h.members.Set(groupFamily, idBob, tg.ChatMember{Status: tg.MemberMember})
	return e
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("body (%d): %v: %s", w.Code, err, w.Body)
	}
	return v
}

func wantStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d; body %s", w.Code, status, w.Body)
	}
}

func chatPath(chat int64, rest string) string {
	return fmt.Sprintf("/api/chats/%d%s", chat, rest)
}

func (e *writeEnv) todo(id int64) model.Todo {
	e.t.Helper()
	td, err := e.st.GetTodo(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return td
}

func (e *writeEnv) pending(todoID int64) []model.Reminder {
	e.t.Helper()
	// The store has no list of the reminders of an item: read the rows by
	// ID until a few IDs in a row are missing.
	var out []model.Reminder
	for id, miss := int64(1), 0; miss < 3; id++ {
		r, err := e.st.GetReminder(context.Background(), id)
		if errors.Is(err, store.ErrNotFound) {
			miss++
			continue
		} else if err != nil {
			e.t.Fatal(err)
		}
		miss = 0
		if r.TodoID == todoID && r.State == model.ReminderPending {
			out = append(out, r)
		}
	}
	return out
}

// countWaker counts the calls of Wake.
type countWaker struct{ n atomic.Int32 }

func (c *countWaker) Wake() { c.n.Add(1) }

func TestAddItem(t *testing.T) {
	e := newWriteEnv(t)
	// Persian digits, spaces, and a date in Ann's time zone (UTC+3:30):
	// tomorrow 18:00 in Tehran is 14:30 UTC.
	w := e.do("POST", chatPath(idAnn, "/items"), `{"text":"  buy milk tomorrow ۱۸:۰۰  "}`, bearer(e.ann))
	wantStatus(t, w, 201)
	it := decodeBody[itemDTO](t, w)
	if it.Text != "buy milk" || it.ChatID != idAnn || it.CreatedBy != idAnn || it.Assignee != nil {
		t.Fatalf("item %+v", it)
	}
	if want := time.Date(2026, 3, 2, 14, 30, 0, 0, time.UTC); it.DueAt == nil || !it.DueAt.Equal(want) {
		t.Fatalf("due %v, want %v", it.DueAt, want)
	}
	if it.DueText == nil || !strings.Contains(*it.DueText, "۱۸:۰۰") {
		t.Errorf("due_text %v: not in Farsi digits", it.DueText)
	}
	if rs := e.pending(it.ID); len(rs) != 1 || rs[0].Kind != model.ReminderDue {
		t.Errorf("reminders %+v", rs)
	}
	// The item keeps the digits as the user wrote them, as /add does
	// (§5.4 step 1), in the answer and in the database.
	w = e.do("POST", chatPath(idAnn, "/items"), `{"text":"خرید ۲ نان"}`, bearer(e.ann))
	wantStatus(t, w, 201)
	it = decodeBody[itemDTO](t, w)
	if it.Text != "خرید ۲ نان" || it.DueAt != nil {
		t.Errorf("item %+v", it)
	}
	if td := e.todo(it.ID); td.Text != "خرید ۲ نان" {
		t.Errorf("stored text %q", td.Text)
	}
	// Persian digits in the text and in the time: the time still parses.
	// 09:30 in Tehran is 06:00 UTC.
	w = e.do("POST", chatPath(idAnn, "/items"), `{"text":"خرید ۲ نان tomorrow ۰۹:۳۰"}`, bearer(e.ann))
	wantStatus(t, w, 201)
	it = decodeBody[itemDTO](t, w)
	if want := time.Date(2026, 3, 2, 6, 0, 0, 0, time.UTC); it.Text != "خرید ۲ نان" || it.DueAt == nil || !it.DueAt.Equal(want) {
		t.Errorf("item %+v, want due %v", it, want)
	}
	if td := e.todo(it.ID); td.Text != "خرید ۲ نان" {
		t.Errorf("stored text %q", td.Text)
	}
	// A repeat.
	w = e.do("POST", chatPath(idAnn, "/items"), `{"text":"water the plants every monday 07:00"}`, bearer(e.ann))
	wantStatus(t, w, 201)
	if it := decodeBody[itemDTO](t, w); it.Recur == "" || it.RecurText == "" || it.DueAt == nil {
		t.Errorf("repeat %+v", it)
	}
}

func TestAddItemErrors(t *testing.T) {
	e := newWriteEnv(t)
	p := chatPath(idAnn, "/items")
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"empty", `{"text":"   "}`, 422, "empty_text"},
		{"no text field", `{}`, 422, "empty_text"},
		{"only a date", `{"text":"tomorrow 18:00"}`, 422, "empty_text"},
		{"1001 runes", `{"text":"` + strings.Repeat("ب", 1001) + `"}`, 422, "too_long"},
		{"unknown assignee", `{"text":"x","assignee_id":12345}`, 422, "unknown_assignee"},
		{"named, not allowlisted", `{"text":"x","assignee_id":777}`, 422, "unknown_assignee"},
		{"self", fmt.Sprintf(`{"text":"x","assignee_id":%d}`, idAnn), 422, "unknown_assignee"},
		{"unknown field", `{"text":"x","chat_id":1}`, 400, "bad_request"},
		{"not JSON", `text`, 400, "bad_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantErr(t, e.do("POST", p, tc.body, bearer(e.ann)), tc.status, tc.code)
		})
	}
	// 1000 Farsi letters are 2000 bytes, and still pass: the limit is in runes.
	w := e.do("POST", p, `{"text":"`+strings.Repeat("ب", 1000)+`"}`, bearer(e.ann))
	wantStatus(t, w, 201)
	// 1000 runes after the normalisation of the digits.
	w = e.do("POST", p, `{"text":"`+strings.Repeat("۱", 1000)+`"}`, bearer(e.ann))
	wantStatus(t, w, 201)

	wantErr(t, e.do("POST", p, `{"text":"x"}`), 401, "unauthenticated")
	wantErr(t, e.do("POST", p, `{"text":"x"}`, bearer(e.ann), delHeader("X-Todo-Request")), 403, "csrf")
	wantErr(t, e.do("POST", p, `{"text":"`+strings.Repeat("a", 17<<10)+`"}`, bearer(e.ann)), 413, "too_large")
}

func TestAddItemWithAssignee(t *testing.T) {
	e := newWriteEnv(t)
	// Ann gives an item to Bob from the group.
	w := e.do("POST", chatPath(groupFamily, "/items"), fmt.Sprintf(`{"text":"fix the door","assignee_id":%d}`, idBob), bearer(e.ann))
	wantStatus(t, w, 201)
	it := decodeBody[itemDTO](t, w)
	if it.ChatID != groupFamily || it.Assignee == nil || *it.Assignee != (assigneeDTO{ID: idBob, Username: "bob"}) {
		t.Fatalf("item %+v", it)
	}
	// The group keeps its stored title: the web never sends a title.
	if c, err := e.st.GetChat(context.Background(), groupFamily); err != nil || c.Title != "Family" {
		t.Fatalf("chat %+v %v", c, err)
	}
	// assignee_id 0 is nobody.
	w = e.do("POST", chatPath(idAnn, "/items"), `{"text":"x","assignee_id":0}`, bearer(e.ann))
	wantStatus(t, w, 201)
	if it := decodeBody[itemDTO](t, w); it.Assignee != nil {
		t.Errorf("assignee %+v", it.Assignee)
	}
}

// openItem adds an item through the API and returns it.
func (e *writeEnv) openItem(tok string, chat int64, text string) itemDTO {
	e.t.Helper()
	w := e.do("POST", chatPath(chat, "/items"), `{"text":"`+text+`"}`, bearer(tok))
	wantStatus(e.t, w, 201)
	return decodeBody[itemDTO](e.t, w)
}

func TestDoneItem(t *testing.T) {
	e := newWriteEnv(t)
	it := e.openItem(e.ann, idAnn, "call the bank tomorrow 10:00")
	p := chatPath(idAnn, fmt.Sprintf("/items/%d/done", it.ID))
	w := e.do("POST", p, `{}`, bearer(e.ann))
	wantStatus(t, w, 200)
	resp := decodeBody[doneResponse](t, w)
	if resp.StillOpen || resp.Item.ID != it.ID {
		t.Fatalf("done %+v", resp)
	}
	if td := e.todo(it.ID); td.Status != model.TodoDone {
		t.Fatalf("status %s", td.Status)
	}
	if rs := e.pending(it.ID); len(rs) != 0 {
		t.Errorf("pending reminders after done: %+v", rs)
	}
	// A second Done: the item is not open any more.
	wantErr(t, e.do("POST", p, `{}`, bearer(e.ann)), 404, "not_found")

	// An empty body is accepted; a body with a field is not.
	it = e.openItem(e.ann, idAnn, "x")
	p = chatPath(idAnn, fmt.Sprintf("/items/%d/done", it.ID))
	wantErr(t, e.do("POST", p, `{"x":1}`, bearer(e.ann)), 400, "bad_request")
	wantStatus(t, e.do("POST", p, "", bearer(e.ann)), 200)

	// Unknown, deleted, of another chat, and a bad ID: all 404.
	wantErr(t, e.do("POST", chatPath(idAnn, "/items/99999/done"), `{}`, bearer(e.ann)), 404, "not_found")
	wantErr(t, e.do("POST", chatPath(idAnn, "/items/abc/done"), `{}`, bearer(e.ann)), 404, "not_found")
	bobs := e.openItem(e.bob, idBob, "bob's own")
	wantErr(t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/done", bobs.ID)), `{}`, bearer(e.ann)), 404, "not_found")
	if td := e.todo(bobs.ID); td.Status != model.TodoOpen {
		t.Fatalf("an item of another chat changed: %s", td.Status)
	}
}

func TestDoneRepeatingItemStaysOpen(t *testing.T) {
	e := newWriteEnv(t)
	// Due in 2 hours, so Complete moves it to the next week.
	td := e.addTodo(model.Todo{ChatID: idAnn, Text: "standup", DueAt: at(2026, 3, 1, 14, 0),
		Recur: "weekly sun 17:30", TZ: "Asia/Tehran", CreatedBy: idAnn})
	w := e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/done", td.ID)), `{}`, bearer(e.ann))
	wantStatus(t, w, 200)
	resp := decodeBody[doneResponse](t, w)
	if want := time.Date(2026, 3, 8, 14, 0, 0, 0, time.UTC); !resp.StillOpen || resp.Item.DueAt == nil || !resp.Item.DueAt.Equal(want) {
		t.Fatalf("done %+v, want open at %v", resp, want)
	}
}

func TestDeleteItem(t *testing.T) {
	e := newWriteEnv(t)
	it := e.openItem(e.ann, idAnn, "old thing tomorrow 10:00")
	p := chatPath(idAnn, fmt.Sprintf("/items/%d", it.ID))
	w := e.do("DELETE", p, "", bearer(e.ann))
	wantStatus(t, w, 204)
	if w.Body.Len() != 0 {
		t.Errorf("body %q", w.Body)
	}
	if td := e.todo(it.ID); td.Status != model.TodoDeleted {
		t.Fatalf("status %s", td.Status)
	}
	if rs := e.pending(it.ID); len(rs) != 0 {
		t.Errorf("pending reminders after delete: %+v", rs)
	}
	wantErr(t, e.do("DELETE", p, "", bearer(e.ann)), 404, "not_found")
	wantErr(t, e.do("DELETE", chatPath(idAnn, "/items/99999"), "", bearer(e.ann)), 404, "not_found")
	wantErr(t, e.do("DELETE", chatPath(idAnn, "/items/1x"), "", bearer(e.ann)), 404, "not_found")
	bobs := e.openItem(e.bob, idBob, "bob's own")
	wantErr(t, e.do("DELETE", chatPath(idAnn, fmt.Sprintf("/items/%d", bobs.ID)), "", bearer(e.ann)), 404, "not_found")
	if td := e.todo(bobs.ID); td.Status != model.TodoOpen {
		t.Fatalf("an item of another chat changed: %s", td.Status)
	}
	wantErr(t, e.do("DELETE", p, "", bearer(e.ann), delHeader("X-Todo-Request")), 403, "csrf")
}

func TestRemindPresets(t *testing.T) {
	e := newWriteEnv(t)
	it := e.openItem(e.ann, idAnn, "pay the bill")
	p := chatPath(idAnn, fmt.Sprintf("/items/%d/remind", it.ID))
	now := e.clk.Now()
	for _, tc := range []struct {
		body string
		want time.Time
	}{
		{`{"preset":"10m"}`, now.Add(10 * time.Minute)},
		{`{"preset":"1h"}`, now.Add(time.Hour)},
		// Tomorrow at TODO_DEFAULT_TIME (08:30) in Tehran is 05:00 UTC.
		{`{"preset":"tomorrow"}`, time.Date(2026, 3, 2, 5, 0, 0, 0, time.UTC)},
		// A text, with Persian digits, in Ann's time zone.
		{`{"when":"tomorrow ۲۰:۰۰"}`, time.Date(2026, 3, 2, 16, 30, 0, 0, time.UTC)},
	} {
		w := e.do("POST", p, tc.body, bearer(e.ann))
		wantStatus(t, w, 201)
		resp := decodeBody[remindResponse](t, w)
		if !resp.FireAt.Equal(tc.want) || resp.FireText == "" {
			t.Errorf("%s: %+v, want %v", tc.body, resp, tc.want)
		}
	}
	rs := e.pending(it.ID)
	if len(rs) != 4 {
		t.Fatalf("%d reminders, want 4", len(rs))
	}
	for _, r := range rs {
		if r.Kind != model.ReminderCustom {
			t.Errorf("kind %s", r.Kind)
		}
	}
}

func TestRemindErrors(t *testing.T) {
	e := newWriteEnv(t)
	it := e.openItem(e.ann, idAnn, "pay the bill")
	p := chatPath(idAnn, fmt.Sprintf("/items/%d/remind", it.ID))
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"when":"today 08:00"}`, 422, "in_past"},
		{`{"when":"banana"}`, 422, "bad_time"},
		{`{"when":""}`, 422, "bad_time"},
		{`{"when":"every day 10:00"}`, 422, "bad_time"},
		{`{"preset":"2d"}`, 400, "bad_request"},
		{`{}`, 400, "bad_request"},
		{`{"preset":"10m","when":"tomorrow"}`, 400, "bad_request"},
		{`{"preset":"10m","x":1}`, 400, "bad_request"},
	} {
		wantErr(t, e.do("POST", p, tc.body, bearer(e.ann)), tc.status, tc.code)
	}
	if rs := e.pending(it.ID); len(rs) != 0 {
		t.Errorf("reminders after errors: %+v", rs)
	}
	wantErr(t, e.do("POST", chatPath(idAnn, "/items/99999/remind"), `{"preset":"1h"}`, bearer(e.ann)), 404, "not_found")
	// An item of another chat: 404, and it gets no reminder.
	bobs := e.openItem(e.bob, idBob, "bob's own")
	wantErr(t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/remind", bobs.ID)), `{"preset":"1h"}`, bearer(e.ann)), 404, "not_found")
	if rs := e.pending(bobs.ID); len(rs) != 0 {
		t.Errorf("an item of another chat got reminders: %+v", rs)
	}
	// A done item has no reminders any more.
	wantStatus(t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.ann)), 200)
	wantErr(t, e.do("POST", p, `{"preset":"1h"}`, bearer(e.ann)), 404, "not_found")
}

// TestRemindTomorrowAcrossDST checks "tomorrow" on the days of the DST
// changes in Europe/Berlin (29 Mar and 25 Oct 2026). Each case starts after
// local midnight but before UTC midnight, so a date taken in UTC, or a day
// of 24 hours, gives a wrong answer.
func TestRemindTomorrowAcrossDST(t *testing.T) {
	for _, tc := range []struct {
		name string
		now  time.Time // UTC
		want time.Time // UTC
	}{
		// Sat 28 Mar 23:30 UTC is Sun 29 Mar 00:30 CET. DST starts at 02:00
		// that day; Mon 30 Mar 08:30 CEST is 06:30 UTC.
		{"spring, after local midnight", time.Date(2026, 3, 28, 23, 30, 0, 0, time.UTC), time.Date(2026, 3, 30, 6, 30, 0, 0, time.UTC)},
		// Sat 28 Mar 12:00 CET; Sun 29 Mar 08:30 CEST is 06:30 UTC.
		{"spring, the day before", time.Date(2026, 3, 28, 11, 0, 0, 0, time.UTC), time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)},
		// Sat 24 Oct 22:30 UTC is Sun 25 Oct 00:30 CEST. DST ends at 03:00
		// that day; Mon 26 Oct 08:30 CET is 07:30 UTC.
		{"autumn, after local midnight", time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC), time.Date(2026, 10, 26, 7, 30, 0, 0, time.UTC)},
		// Sat 24 Oct 12:00 CEST; Sun 25 Oct 08:30 CET is 07:30 UTC.
		{"autumn, the day before", time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC), time.Date(2026, 10, 25, 7, 30, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newWriteEnv(t)
			e.setUser(idBob, i18n.En, calendar.Gregorian, "Europe/Berlin")
			e.clk.Set(tc.now)
			tok := e.signIn(userBob) // the prefs of a session are read on each request
			it := e.openItem(tok, idBob, "water the plants")
			w := e.do("POST", chatPath(idBob, fmt.Sprintf("/items/%d/remind", it.ID)), `{"preset":"tomorrow"}`, bearer(tok))
			wantStatus(t, w, 201)
			if resp := decodeBody[remindResponse](t, w); !resp.FireAt.Equal(tc.want) {
				t.Fatalf("fire_at %v, want %v", resp.FireAt, tc.want)
			}
		})
	}
}

// TestWriteRoutesNeedMembership calls every route of W8 under {chat} as a
// user who is not a member of the group (403), for a group the bot does not
// know (404), and for the private chat of another user (404). Nothing
// changes.
func TestWriteRoutesNeedMembership(t *testing.T) {
	e := newWriteEnv(t)
	grp := e.openItem(e.bob, groupFamily, "group item")
	e.members.Set(groupFamily, idAnn, tg.ChatMember{Status: tg.MemberLeft})
	bobs := e.openItem(e.bob, idBob, "bob's own")
	routes := []struct{ method, rest, body string }{
		{"GET", "/members", ""},
		{"POST", "/items", `{"text":"x"}`},
		{"POST", "/items/%d/done", `{}`},
		{"DELETE", "/items/%d", ""},
		{"POST", "/items/%d/remind", `{"preset":"1h"}`},
	}
	for _, rt := range routes {
		rest := rt.rest
		if strings.Contains(rest, "%d") {
			rest = fmt.Sprintf(rest, grp.ID)
		}
		t.Run(rt.method+" group "+rt.rest, func(t *testing.T) {
			wantErr(t, e.do(rt.method, chatPath(groupFamily, rest), rt.body, bearer(e.ann)), 403, "not_member")
		})
		t.Run(rt.method+" unknown group "+rt.rest, func(t *testing.T) {
			wantErr(t, e.do(rt.method, chatPath(-100777, rest), rt.body, bearer(e.ann)), 404, "not_found")
		})
		rest = rt.rest
		if strings.Contains(rest, "%d") {
			rest = fmt.Sprintf(rest, bobs.ID)
		}
		t.Run(rt.method+" private chat of another user "+rt.rest, func(t *testing.T) {
			wantErr(t, e.do(rt.method, chatPath(idBob, rest), rt.body, bearer(e.ann)), 404, "not_found")
		})
		t.Run(rt.method+" no session "+rt.rest, func(t *testing.T) {
			wantErr(t, e.do(rt.method, chatPath(idBob, rest), rt.body), 401, "unauthenticated")
		})
	}
	for _, id := range []int64{grp.ID, bobs.ID} {
		if td := e.todo(id); td.Status != model.TodoOpen {
			t.Errorf("item %d changed: %s", id, td.Status)
		}
		if rs := e.pending(id); len(rs) != 0 {
			t.Errorf("item %d got reminders: %+v", id, rs)
		}
	}
	if ts, _, _ := e.svc.List(context.Background(), groupFamily, 0); len(ts) != 3 { // the two of the fixture and "group item"
		t.Errorf("group items: %d", len(ts))
	}
}

// TestWriteInGroup checks the writes of a member in a group.
func TestWriteInGroup(t *testing.T) {
	e := newWriteEnv(t)
	it := e.openItem(e.ann, groupFamily, "buy bread")
	if it.ChatID != groupFamily || it.CreatedBy != idAnn {
		t.Fatalf("item %+v", it)
	}
	wantStatus(t, e.do("POST", chatPath(groupFamily, fmt.Sprintf("/items/%d/remind", it.ID)), `{"preset":"1h"}`, bearer(e.bob)), 201)
	wantStatus(t, e.do("POST", chatPath(groupFamily, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.bob)), 200)
	it = e.openItem(e.ann, groupFamily, "buy cheese")
	wantStatus(t, e.do("DELETE", chatPath(groupFamily, fmt.Sprintf("/items/%d", it.ID)), "", bearer(e.bob)), 204)
}

// TestAssigneeActsFromPrivateChat: the assignee completes and reminds an
// item of a group from the private chat (§5.1, ARCHITECTURE §11).
func TestAssigneeActsFromPrivateChat(t *testing.T) {
	e := newWriteEnv(t)
	w := e.do("POST", chatPath(groupFamily, "/items"), fmt.Sprintf(`{"text":"fix the door","assignee_id":%d}`, idAnn), bearer(e.bob))
	wantStatus(t, w, 201)
	it := decodeBody[itemDTO](t, w)
	e.members.Set(groupFamily, idAnn, tg.ChatMember{Status: tg.MemberLeft}) // the assignee left the group
	wantStatus(t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/remind", it.ID)), `{"preset":"10m"}`, bearer(e.ann)), 201)
	wantStatus(t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/done", it.ID)), `{}`, bearer(e.ann)), 200)
	if td := e.todo(it.ID); td.Status != model.TodoDone {
		t.Fatalf("status %s", td.Status)
	}
}

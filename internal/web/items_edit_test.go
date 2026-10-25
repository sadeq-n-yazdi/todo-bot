package web

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// The fixture of seedFixture: the items by ID.
const (
	itemBuyMilk  = 1 // Ann, due 2 Mar 18:00 Tehran
	itemCallMom  = 2 // Ann, no date
	itemPlants   = 3 // Ann, weekly sun 18:00, zone Asia/Tehran
	itemReport   = 5 // Ann, given to Bob
	itemRent     = 7 // Family, monthly 1 08:00 jalali, zone Asia/Tehran
	itemFlight   = 8 // Family, given to Ann
	itemNotFound = 99999
)

func itemPath(chat, id int64) string { return chatPath(chat, fmt.Sprintf("/items/%d", id)) }

func (e *writeEnv) getEdit(tok string, chat, id int64) editItemDTO {
	e.t.Helper()
	w := e.do("GET", itemPath(chat, id), "", bearer(tok))
	wantStatus(e.t, w, 200)
	return decodeBody[editItemDTO](e.t, w)
}

func (e *writeEnv) patch(tok string, chat, id int64, body string) editItemDTO {
	e.t.Helper()
	w := e.do("PATCH", itemPath(chat, id), body, bearer(tok))
	wantStatus(e.t, w, 200)
	return decodeBody[editItemDTO](e.t, w)
}

func TestGetItemForEdit(t *testing.T) {
	e := newWriteEnv(t)
	it := e.getEdit(e.ann, idAnn, itemBuyMilk)
	if it.ID != itemBuyMilk || it.Text != "buy milk" || it.Status != model.TodoOpen || it.DueText == nil {
		t.Fatalf("item %+v", it)
	}
	// 14:30 UTC is 18:00 in Tehran, in ASCII digits.
	if it.WhenText != "2026-03-02 18:00" {
		t.Errorf("when_text %q", it.WhenText)
	}
	if it.Note != "" {
		t.Errorf("note %q in a GET", it.Note)
	}
	// The raw JSON has the two fields of the form and no note.
	w := e.do("GET", itemPath(idAnn, itemCallMom), "", bearer(e.ann))
	wantStatus(t, w, 200)
	body := w.Body.String()
	if !strings.Contains(body, `"status":"open"`) || !strings.Contains(body, `"when_text":""`) || strings.Contains(body, `"note"`) {
		t.Errorf("body %s", body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", w.Header().Get("Cache-Control"))
	}
	// A weekly rule in the zone of Ann: a phrase that gives the same rule.
	it = e.getEdit(e.ann, idAnn, itemPlants)
	if it.WhenText == "" {
		t.Fatal("no when_text for a weekly rule")
	}
	wantSameRule(t, it.WhenText, "weekly sun 18:00", calendar.Gregorian)
	// A Jalali monthly rule of Tehran for Bob (UTC, Gregorian): no phrase
	// gives the same schedule.
	if it := e.getEdit(e.bob, groupFamily, itemRent); it.WhenText != "" {
		t.Errorf("when_text %q, want empty", it.WhenText)
	}
	// The assignee opens the form from "Assigned to me": the actor is her
	// private chat.
	if it := e.getEdit(e.ann, idAnn, itemFlight); it.ChatID != groupFamily || it.Text != "book the flight" {
		t.Errorf("assigned item %+v", it)
	}
	// Unknown, of another chat, and a bad ID: 404.
	bobs := e.openItem(e.bob, idBob, "bob's own")
	for _, p := range []string{itemPath(idAnn, itemNotFound), itemPath(idAnn, bobs.ID), chatPath(idAnn, "/items/abc")} {
		wantErr(t, e.do("GET", p, "", bearer(e.ann)), 404, "not_found")
	}
}

func wantSameRule(t *testing.T, phrase, rule string, cal calendar.Kind) {
	t.Helper()
	tehran, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	res, err := nlparse.Parser{DefaultHour: 9}.Parse(phrase, testNow.In(tehran), cal)
	if err != nil || res.Rule == nil || res.Rule.String() != rule || res.Text != "" {
		t.Errorf("%q parses to %+v %v, want %s", phrase, res, err, rule)
	}
}

// Each kind of rule that has a phrase gives it back through the parser;
// every-N and a rule of another zone give "".
func TestWhenTextRoundTrip(t *testing.T) {
	e := newWriteEnv(t)
	cases := []struct {
		recur string
		cal   calendar.Kind
		empty bool
	}{
		{"daily 07:15", calendar.Gregorian, false},
		{"weekly mon,wed,fri 18:00", calendar.Gregorian, false},
		{"monthly 1 08:00", calendar.Gregorian, false},
		{"monthly 22 08:00", calendar.Gregorian, false},
		{"monthly 13 08:00 jalali", calendar.Jalali, false},
		{"monthly fri#last 18:00", calendar.Gregorian, false},
		{"monthly tue#2 09:00", calendar.Gregorian, false},
		{"yearly 03-15 18:00", calendar.Gregorian, false},
		{"yearly 01-01 10:00 jalali", calendar.Jalali, false},
		{"every 3d 2026-03-01 18:00", calendar.Gregorian, true},
		{"monthly 1 08:00 jalali", calendar.Gregorian, true}, // another calendar
	}
	for _, c := range cases {
		e.setUser(idAnn, i18n.Fa, c.cal, "Asia/Tehran")
		td := e.addTodo(model.Todo{ChatID: idAnn, Text: c.recur, DueAt: at(2026, 3, 10, 6, 0), Recur: c.recur, TZ: "Asia/Tehran", CreatedBy: idAnn})
		got := e.getEdit(e.ann, idAnn, td.ID).WhenText
		if c.empty {
			if got != "" {
				t.Errorf("%s: when_text %q, want empty", c.recur, got)
			}
			continue
		}
		if got == "" {
			t.Errorf("%s: no when_text", c.recur)
			continue
		}
		wantSameRule(t, got, c.recur, c.cal)
	}
	// A rule of another zone than the zone of the user.
	e.setUser(idAnn, i18n.Fa, calendar.Gregorian, "Asia/Tehran")
	td := e.addTodo(model.Todo{ChatID: idAnn, Text: "x", DueAt: at(2026, 3, 10, 6, 0), Recur: "daily 07:15", TZ: "UTC", CreatedBy: idAnn})
	if got := e.getEdit(e.ann, idAnn, td.ID).WhenText; got != "" {
		t.Errorf("another zone: when_text %q", got)
	}
	// A Jalali user gets a Jalali date, which the parser reads back.
	e.setUser(idAnn, i18n.Fa, calendar.Jalali, "Asia/Tehran")
	if got := e.getEdit(e.ann, idAnn, itemBuyMilk).WhenText; got != "1404-12-11 18:00" {
		t.Errorf("jalali when_text %q", got)
	}
	// A date with no time (23:59) gives only the date.
	e.setUser(idAnn, i18n.Fa, calendar.Gregorian, "Asia/Tehran")
	td = e.addTodo(model.Todo{ChatID: idAnn, Text: "y", DueAt: at(2026, 3, 1, 20, 29), CreatedBy: idAnn}) // 23:59 Tehran
	if got := e.getEdit(e.ann, idAnn, td.ID).WhenText; got != "2026-03-01" {
		t.Errorf("date only: when_text %q", got)
	}
}

func TestEditDoneOrDeletedGives404(t *testing.T) {
	e := newWriteEnv(t)
	done := e.openItem(e.ann, idAnn, "done one")
	wantStatus(t, e.do("POST", chatPath(idAnn, fmt.Sprintf("/items/%d/done", done.ID)), `{}`, bearer(e.ann)), 200)
	gone := e.openItem(e.ann, idAnn, "deleted one")
	wantStatus(t, e.do("DELETE", itemPath(idAnn, gone.ID), "", bearer(e.ann)), 204)
	for _, id := range []int64{done.ID, gone.ID} {
		wantErr(t, e.do("GET", itemPath(idAnn, id), "", bearer(e.ann)), 404, "not_found")
		wantErr(t, e.do("PATCH", itemPath(idAnn, id), `{"text":"again"}`, bearer(e.ann)), 404, "not_found")
	}
	if td := e.todo(done.ID); td.Text != "done one" || td.Status != model.TodoDone {
		t.Errorf("done item changed: %+v", td)
	}
}

func TestEditNotMember(t *testing.T) {
	e := newWriteEnv(t)
	e.members.Set(groupFamily, idAnn, tg.ChatMember{Status: tg.MemberLeft})
	for _, rt := range []struct{ method, body string }{{"GET", ""}, {"PATCH", `{"text":"mine now"}`}} {
		w := e.do(rt.method, itemPath(groupFamily, itemRent), rt.body, bearer(e.ann))
		if w.Code != 403 && w.Code != 404 {
			t.Errorf("%s: %d %s, want 403 or 404", rt.method, w.Code, w.Body)
		}
	}
	if td := e.todo(itemRent); td.Text != "pay the rent" {
		t.Errorf("text %q", td.Text)
	}
	// A member of the group can.
	if it := e.patch(e.bob, groupFamily, itemRent, `{"text":"pay the rent today"}`); it.Text != "pay the rent today" {
		t.Errorf("item %+v", it)
	}
}

// pendingIDs gives the pending reminders of an item by kind.
func (e *writeEnv) pendingByKind(todoID int64) map[model.ReminderKind][]model.Reminder {
	out := map[model.ReminderKind][]model.Reminder{}
	for _, r := range e.pending(todoID) {
		out[r.Kind] = append(out[r.Kind], r)
	}
	return out
}

func TestPatchKeepsMissingFields(t *testing.T) {
	e := newWriteEnv(t)
	w := e.do("POST", chatPath(idAnn, "/items"), fmt.Sprintf(`{"text":"pay bills tomorrow 10:00","assignee_id":%d}`, idBob), bearer(e.ann))
	wantStatus(t, w, 201)
	it := decodeBody[itemDTO](t, w)
	before := e.todo(it.ID)
	rems := e.pending(it.ID)
	if len(rems) != 1 {
		t.Fatalf("reminders %+v", rems)
	}
	e.send.Reset()

	// Text only: the date, the repeat, the assignee and the reminders stay.
	got := e.patch(e.ann, idAnn, it.ID, `{"text":"  pay all bills  "}`)
	after := e.todo(it.ID)
	if got.Text != "pay all bills" || after.Text != "pay all bills" {
		t.Fatalf("text %q / %q", got.Text, after.Text)
	}
	if !after.DueAt.Equal(*before.DueAt) || after.Recur != before.Recur || after.AssigneeID != idBob || after.Num != before.Num {
		t.Errorf("after %+v, before %+v", after, before)
	}
	if now := e.pending(it.ID); len(now) != 1 || now[0].ID != rems[0].ID {
		t.Errorf("reminders %+v, want %+v", now, rems)
	}
	// The text is not parsed for a date.
	if got := e.patch(e.ann, idAnn, it.ID, `{"text":"pay bills friday 9:00"}`); got.Text != "pay bills friday 9:00" || !got.DueAt.Equal(*before.DueAt) {
		t.Errorf("item %+v", got)
	}

	// An empty body changes nothing.
	got = e.patch(e.ann, idAnn, it.ID, `{}`)
	if got.Text != "pay bills friday 9:00" || got.Assignee == nil || got.Assignee.ID != idBob || got.DueAt == nil {
		t.Errorf("empty patch: %+v", got)
	}

	// Assignee 0: nobody. The text and the date stay.
	got = e.patch(e.ann, idAnn, it.ID, `{"assignee_id":0}`)
	if got.Assignee != nil || got.Text != "pay bills friday 9:00" || got.DueAt == nil {
		t.Errorf("cleared assignee: %+v", got)
	}
	if e.todo(it.ID).AssigneeID != 0 {
		t.Error("assignee not cleared")
	}
	// None of these sent a message to Bob.
	if msgs := e.send.Messages(); len(msgs) != 0 {
		t.Errorf("messages %+v", msgs)
	}
}

func TestPatchWhen(t *testing.T) {
	e := newWriteEnv(t)
	ctx := context.Background()
	it := e.openItem(e.ann, idAnn, "call the bank tomorrow 10:00")
	a := service.Actor{UserID: idAnn, ChatID: idAnn, ChatKind: model.ChatPrivate}
	custom, err := e.svc.Remind(ctx, a, it.ID, testNow.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	snooze := &model.Reminder{TodoID: it.ID, ChatID: idAnn, FireAt: testNow.Add(time.Hour), Kind: model.ReminderSnooze, State: model.ReminderPending, CreatedAt: testNow}
	if err := e.st.InsertReminder(ctx, snooze); err != nil {
		t.Fatal(err)
	}
	if k := e.pendingByKind(it.ID); len(k[model.ReminderDue]) != 1 || len(k[model.ReminderSnooze]) != 1 || len(k[model.ReminderCustom]) != 1 {
		t.Fatalf("reminders before %+v", k)
	}
	wakes := &countWaker{}
	e.svc.SetWaker(wakes)

	// A new date, in Persian digits: 5 Mar 10:00 in Tehran is 06:30 UTC.
	got := e.patch(e.ann, idAnn, it.ID, `{"when":"۲۰۲۶-۰۳-۰۵ ۱۰:۰۰"}`)
	want := time.Date(2026, 3, 5, 6, 30, 0, 0, time.UTC)
	if got.DueAt == nil || !got.DueAt.Equal(want) || got.Recur != "" || got.Text != "call the bank" {
		t.Fatalf("item %+v, want due %v", got, want)
	}
	k := e.pendingByKind(it.ID)
	if len(k[model.ReminderSnooze]) != 0 {
		t.Errorf("snooze kept: %+v", k)
	}
	if c := k[model.ReminderCustom]; len(c) != 1 || c[0].ID != custom.ID {
		t.Errorf("custom reminders %+v, want %d", c, custom.ID)
	}
	if d := k[model.ReminderDue]; len(d) != 1 || !d[0].FireAt.Equal(want) {
		t.Errorf("due reminders %+v", d)
	}
	if wakes.n.Load() == 0 {
		t.Error("the scheduler was not woken")
	}

	// A repeat: the next Monday at 09:00 in Tehran.
	got = e.patch(e.ann, idAnn, it.ID, `{"when":"every monday 9:00"}`)
	want = time.Date(2026, 3, 2, 5, 30, 0, 0, time.UTC)
	if got.Recur != "weekly mon 09:00" || got.DueAt == nil || !got.DueAt.Equal(want) {
		t.Fatalf("repeat %+v, want due %v", got, want)
	}
	if got.WhenText == "" {
		t.Error("no when_text after a repeat")
	}

	// null removes the date and the repeat, and the due reminder. The
	// custom reminder stays.
	got = e.patch(e.ann, idAnn, it.ID, `{"when":null}`)
	if got.DueAt != nil || got.Recur != "" || got.WhenText != "" {
		t.Fatalf("no date: %+v", got)
	}
	td := e.todo(it.ID)
	if td.DueAt != nil || td.Recur != "" {
		t.Errorf("stored %+v", td)
	}
	k = e.pendingByKind(it.ID)
	if len(k[model.ReminderDue]) != 0 || len(k[model.ReminderCustom]) != 1 {
		t.Errorf("reminders after null %+v", k)
	}

	// A repeat that starts in the past is moved forward, not refused.
	got = e.patch(e.ann, idAnn, it.ID, `{"when":"every day 8:00"}`)
	if got.DueAt == nil || !got.DueAt.After(testNow) {
		t.Errorf("repeat in the past: %+v", got)
	}
}

func TestPatchErrors(t *testing.T) {
	e := newWriteEnv(t)
	it := e.openItem(e.ann, idAnn, "call the bank tomorrow 10:00")
	before := e.todo(it.ID)
	p := itemPath(idAnn, it.ID)
	for _, c := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"when":"nonsense"}`, 422, "bad_time"},
		{`{"when":"tomorrow blah"}`, 422, "bad_time"},
		{`{"when":"  "}`, 422, "bad_time"},
		{`{"when":"2026-02-01 10:00"}`, 422, "in_past"},
		{`{"when":"today 9:00"}`, 422, "in_past"}, // 15:30 in Tehran now
		{`{"when":5}`, 400, "bad_request"},
		{`{"when":{"x":1}}`, 400, "bad_request"},
		{`{"text":"   "}`, 422, "empty_text"},
		{`{"text":null,"when":"nonsense"}`, 422, "bad_time"},
		{`{"text":"` + strings.Repeat("a", maxItemRunes+1) + `"}`, 422, "too_long"},
		{`{"assignee_id":999}`, 422, "unknown_assignee"},
		{fmt.Sprintf(`{"assignee_id":%d}`, idAnn), 422, "unknown_assignee"}, // as in Add
		{`{"assignee_id":"x"}`, 400, "bad_request"},
		{`{"status":"done"}`, 400, "bad_request"},
		{`not json`, 400, "bad_request"},
		// A valid field with a bad one: nothing is saved.
		{`{"text":"new text","when":"nonsense"}`, 422, "bad_time"},
	} {
		w := e.do("PATCH", p, c.body, bearer(e.ann))
		if w.Code != c.status || errCode(t, w) != c.code {
			t.Errorf("%s: %d %s, want %d %s", c.body, w.Code, w.Body, c.status, c.code)
		}
	}
	after := e.todo(it.ID)
	if after.Text != before.Text || !after.DueAt.Equal(*before.DueAt) || after.AssigneeID != 0 {
		t.Errorf("after %+v, before %+v", after, before)
	}
	// A date only for today, after the default time, is the end of today:
	// it is in the future, so it is not in_past.
	if got := e.patch(e.ann, idAnn, it.ID, `{"when":"today"}`); got.DueAt == nil {
		t.Errorf("today: %+v", got)
	}
}

// The assignee edits an item of the group from her private chat.
func TestPatchAssignedItemFromPrivateChat(t *testing.T) {
	e := newWriteEnv(t)
	got := e.patch(e.ann, idAnn, itemFlight, `{"text":"book the flight to Shiraz"}`)
	if got.ChatID != groupFamily || got.Text != "book the flight to Shiraz" || got.Assignee == nil || got.Assignee.ID != idAnn {
		t.Errorf("item %+v", got)
	}
}

func TestRulePhraseHasNoPhraseForEveryN(t *testing.T) {
	r, err := recur.Parse("every 3d 2026-03-01 18:00")
	if err != nil {
		t.Fatal(err)
	}
	if p := rulePhrase(r); p != "" {
		t.Errorf("phrase %q", p)
	}
	for n, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 31: "31st"} {
		if got := ordinal(n); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", n, got, want)
		}
	}
}

package service

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

var stranger = Actor{UserID: 50, ChatID: 50, ChatKind: model.ChatPrivate}

func sp(s string) *string { return &s }
func ip(i int64) *int64   { return &i }

// pending lists the pending reminders of an item, in fire order. It only
// reads, so it does not change what the scheduler can claim later.
func (e *env) pending(todoID int64) []model.Reminder {
	e.t.Helper()
	rows, err := e.raw().Query(`SELECT id FROM reminders WHERE todo_id = ? AND state = 'pending' ORDER BY fire_at, id`, todoID)
	e.must(err)
	var ids []int64
	for rows.Next() {
		var id int64
		e.must(rows.Scan(&id))
		ids = append(ids, id)
	}
	e.must(rows.Err())
	e.must(rows.Close())
	out := make([]model.Reminder, 0, len(ids))
	for _, id := range ids {
		r, err := e.st.GetReminder(ctx, id)
		e.must(err)
		out = append(out, r)
	}
	return out
}

func (e *env) byKind(rs []model.Reminder, k model.ReminderKind) []model.Reminder {
	var out []model.Reminder
	for _, r := range rs {
		if r.Kind == k {
			out = append(out, r)
		}
	}
	return out
}

func (e *env) get(a Actor, id int64) model.Todo {
	e.t.Helper()
	td, err := e.svc.Todo(ctx, a, id)
	e.must(err)
	return td
}

func (e *env) startEdit(a Actor, msg int) model.Draft {
	e.t.Helper()
	d := model.Draft{ChatID: a.ChatID, UserID: a.UserID, MsgID: msg, Step: model.StepConfirm}
	e.must(e.svc.StartDraft(ctx, &d))
	return d
}

func when(due *time.Time, rule string, hasTime bool) *nlparse.Result {
	r := &nlparse.Result{Due: due, HasTime: hasTime}
	if rule != "" {
		rr, err := recur.Parse(rule)
		if err != nil {
			panic(err)
		}
		r.Rule = &rr
	}
	return r
}

func TestEditTextOnlyKeepsDueAndReminders(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "milk", at(15, 0), nil)
	rem := e.mustRemind(td.ID, at(16, 0).UTC())
	e.clk.Advance(time.Minute)

	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{Text: sp("  oat milk  ")})
	e.must(err)
	if got.Text != "oat milk" || got.Num != td.Num || got.TZ != td.TZ || !got.UpdatedAt.Equal(e.clk.Now()) {
		t.Fatalf("got %+v", got)
	}
	st := e.get(alice, td.ID)
	if st.Text != "oat milk" || st.DueAt == nil || !st.DueAt.Equal(*at(15, 0)) {
		t.Fatalf("stored %+v", st)
	}
	ps := e.pending(td.ID)
	if len(ps) != 2 || len(e.byKind(ps, model.ReminderDue)) != 1 || len(e.byKind(ps, model.ReminderCustom)) != 1 ||
		e.byKind(ps, model.ReminderCustom)[0].ID != rem.ID {
		t.Fatalf("reminders %+v", ps)
	}
}

func TestEditEmptyText(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "milk", nil, nil)
	if _, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{Text: sp("   ")}); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("err = %v", err)
	}
	if e.get(alice, td.ID).Text != "milk" {
		t.Fatal("text changed")
	}
}

func TestEditWhenReplacesDueAndSnoozeKeepsCustom(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "milk", at(15, 0), nil)
	custom := e.mustRemind(td.ID, at(16, 0).UTC())
	dueRem := e.byKind(e.pending(td.ID), model.ReminderDue)[0]
	sn, err := e.svc.Snooze(ctx, alice, dueRem.ID, ui.Snooze1h)
	e.must(err)
	wakes := e.waker.n.Load()

	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(at(18, 0), "", true)})
	e.must(err)
	if got.DueAt == nil || !got.DueAt.Equal(*at(18, 0)) || got.Recur != "" {
		t.Fatalf("got %+v", got)
	}
	if e.waker.n.Load() == wakes {
		t.Fatal("no wake")
	}
	ps := e.pending(td.ID)
	due := e.byKind(ps, model.ReminderDue)
	if len(ps) != 2 || len(due) != 1 || !due[0].FireAt.Equal(*at(18, 0)) ||
		len(e.byKind(ps, model.ReminderCustom)) != 1 || e.byKind(ps, model.ReminderCustom)[0].ID != custom.ID {
		t.Fatalf("reminders %+v", ps)
	}
	if r, err := e.st.GetReminder(ctx, sn.ID); err != nil || r.State == model.ReminderPending {
		t.Fatalf("snooze %+v %v", r, err)
	}
}

func TestEditOnceToRepeatingAndBack(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "pills", at(15, 0), nil)
	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(at(18, 0), "daily 18:00", true)})
	e.must(err)
	if got.Recur != "daily 18:00" || !got.DueAt.Equal(*at(18, 0)) {
		t.Fatalf("got %+v", got)
	}
	ps := e.pending(td.ID)
	if len(ps) != 1 || !ps[0].FireAt.Equal(*at(18, 0)) {
		t.Fatalf("reminders %+v", ps)
	}

	got, err = e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(at(20, 0), "", true)})
	e.must(err)
	if got.Recur != "" || !got.DueAt.Equal(*at(20, 0)) {
		t.Fatalf("got %+v", got)
	}
	ps = e.pending(td.ID)
	if len(ps) != 1 || !ps[0].FireAt.Equal(*at(20, 0)) {
		t.Fatalf("reminders %+v", ps)
	}

	got, err = e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(nil, "", false)})
	e.must(err)
	if got.DueAt != nil || got.Recur != "" || len(e.pending(td.ID)) != 0 {
		t.Fatalf("no date: %+v", got)
	}
}

func TestEditPastStartOfRepeatingMovesForward(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "pills", nil, nil)
	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(at(9, 0), "daily 09:00", true)})
	e.must(err)
	want := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	if got.DueAt == nil || !got.DueAt.Equal(want) {
		t.Fatalf("due = %v, want %v", got.DueAt, want)
	}
	ps := e.pending(td.ID)
	if len(ps) != 1 || !ps[0].FireAt.Equal(want) {
		t.Fatalf("reminders %+v", ps)
	}
}

func TestEditPastOnceDueHasNoReminderAndNoError(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "milk", at(15, 0), nil)
	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(at(9, 0), "", true)})
	e.must(err)
	if got.DueAt == nil || !got.DueAt.Equal(*at(9, 0)) || len(e.pending(td.ID)) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestEditSentinelCreatesNoReminder(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "milk", at(15, 0), nil)
	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(at(23, 59), "", false)})
	e.must(err)
	if got.DueAt == nil || !got.DueAt.Equal(*at(23, 59)) {
		t.Fatalf("got %+v", got)
	}
	if ps := e.pending(td.ID); len(ps) != 0 {
		t.Fatalf("reminders %+v", ps)
	}
}

func TestEditAssignee(t *testing.T) {
	e := newEnv(t)
	e.must(e.note(8, "Bob_X"))
	td := e.addFor(aliceG, 0, "kid", at(15, 0), "")

	got, err := e.svc.UpdateTodo(ctx, aliceG, td.ID, TodoPatch{AssigneeID: ip(8)})
	e.must(err)
	if got.AssigneeID != 8 || got.AssigneeName != "bob_x" {
		t.Fatalf("set: %+v", got)
	}
	if st := e.get(aliceG, td.ID); st.AssigneeID != 8 || st.AssigneeName != "bob_x" {
		t.Fatalf("stored: %+v", st)
	}
	// A text-only edit keeps the assignee and fills the name.
	got, err = e.svc.UpdateTodo(ctx, aliceG, td.ID, TodoPatch{Text: sp("kid up")})
	e.must(err)
	if got.AssigneeID != 8 || got.AssigneeName != "bob_x" {
		t.Fatalf("kept: %+v", got)
	}
	// The assignee changes to a new user: the name is the new one.
	e.must(e.note(9, "Carol_Y"))
	got, err = e.svc.UpdateTodo(ctx, aliceG, td.ID, TodoPatch{AssigneeID: ip(9)})
	e.must(err)
	if got.AssigneeID != 9 || got.AssigneeName != "carol_y" {
		t.Fatalf("new: %+v", got)
	}
	// Unknown user.
	_, err = e.svc.UpdateTodo(ctx, aliceG, td.ID, TodoPatch{AssigneeID: ip(999)})
	if !errors.Is(err, ErrUnknownAssignee) || errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if e.get(aliceG, td.ID).AssigneeID != 9 {
		t.Fatal("assignee changed on error")
	}
	// Cleared.
	got, err = e.svc.UpdateTodo(ctx, aliceG, td.ID, TodoPatch{AssigneeID: ip(0)})
	e.must(err)
	if got.AssigneeID != 0 || got.AssigneeName != "" || e.get(aliceG, td.ID).AssigneeID != 0 {
		t.Fatalf("cleared: %+v", got)
	}
}

func TestEditAssigneeOwnChatStoresZero(t *testing.T) {
	e := newEnv(t)
	e.must(e.note(8, "Bob_X"))
	td := e.add(alice, "milk", nil, nil)
	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{AssigneeID: ip(alice.ChatID)})
	e.must(err)
	if got.AssigneeID != 0 || e.get(alice, td.ID).AssigneeID != 0 {
		t.Fatalf("got %+v", got)
	}
	// The same ID in a group is a real assignee.
	g := e.addFor(aliceG, 0, "x", nil, "")
	e.must(e.note(alice.UserID, "alice"))
	got, err = e.svc.UpdateTodo(ctx, aliceG, g.ID, TodoPatch{AssigneeID: ip(alice.UserID)})
	e.must(err)
	if got.AssigneeID != alice.UserID {
		t.Fatalf("group self: %+v", got)
	}
}

func TestEditDoneOrDeletedKeepsDraft(t *testing.T) {
	for _, kind := range []string{"done", "deleted"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			td := e.add(alice, "milk", at(15, 0), nil)
			d := e.startEdit(alice, 5)
			if kind == "done" {
				_, _, err := e.svc.Complete(ctx, alice, td.ID)
				e.must(err)
			} else {
				e.must(e.svc.Delete(ctx, alice, td.ID))
			}
			_, err := e.svc.SaveEditDraft(ctx, d, alice, td.ID, TodoPatch{Text: sp("new")})
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v", err)
			}
			if _, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{Text: sp("new")}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("UpdateTodo err = %v", err)
			}
			kept, err := e.svc.Draft(ctx, alice.ChatID, alice.UserID)
			e.must(err)
			if kept.Version != d.Version {
				t.Fatalf("draft version %d, want %d", kept.Version, d.Version)
			}
		})
	}
}

func TestEditForeignChat(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "milk", nil, nil)
	d := e.startEdit(stranger, 5)
	if _, err := e.svc.UpdateTodo(ctx, stranger, td.ID, TodoPatch{Text: sp("x")}); !errors.Is(err, ErrForeignChat) {
		t.Fatalf("UpdateTodo: %v", err)
	}
	if _, err := e.svc.SaveEditDraft(ctx, d, stranger, td.ID, TodoPatch{Text: sp("x")}); !errors.Is(err, ErrForeignChat) {
		t.Fatalf("SaveEditDraft: %v", err)
	}
	if _, err := e.svc.Draft(ctx, stranger.ChatID, stranger.UserID); err != nil {
		t.Fatalf("draft lost: %v", err)
	}
	if e.get(alice, td.ID).Text != "milk" {
		t.Fatal("text changed")
	}
}

func TestEditAssigneeMayEditFromPrivateChat(t *testing.T) {
	e := newEnv(t)
	td := e.assigned(at(15, 0), "")
	got, err := e.svc.UpdateTodo(ctx, bob, td.ID, TodoPatch{Text: sp("by bob")})
	e.must(err)
	if got.Text != "by bob" {
		t.Fatalf("got %+v", got)
	}
}

func TestSaveEditDraftStaleVersionWritesNothing(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "milk", at(15, 0), nil)
	d := e.startEdit(alice, 5)
	stale := d
	stale.Version--
	_, err := e.svc.SaveEditDraft(ctx, stale, alice, td.ID, TodoPatch{Text: sp("new"), When: when(at(18, 0), "", true)})
	if !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("err = %v", err)
	}
	st := e.get(alice, td.ID)
	if st.Text != "milk" || !st.DueAt.Equal(*at(15, 0)) {
		t.Fatalf("stored %+v", st)
	}
	if kept, err := e.svc.Draft(ctx, alice.ChatID, alice.UserID); err != nil || kept.Version != d.Version {
		t.Fatalf("draft %+v %v", kept, err)
	}
	// The right version still works, and ends the draft.
	if _, err := e.svc.SaveEditDraft(ctx, d, alice, td.ID, TodoPatch{Text: sp("new")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Draft(ctx, alice.ChatID, alice.UserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft after save: %v", err)
	}
}

// TestSaveEditDraftTwoHandlesOneWrite runs two saves of one draft at the
// same time on two handles, as two nodes would. Exactly one may write.
func TestSaveEditDraftTwoHandlesOneWrite(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	handles := [2]*Svc{e.svc, b}
	const rounds = 10
	for i := range rounds {
		td := e.add(alice, "milk", nil, nil)
		d := e.startEdit(alice, 5)
		texts := [2]string{fmt.Sprintf("a%d", i), fmt.Sprintf("b%d", i)}
		dues := [2]*time.Time{at(15, i), at(16, i)}
		var errs [2]error
		start := make(chan struct{})
		var wg sync.WaitGroup
		for k := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, errs[k] = handles[k].SaveEditDraft(ctx, d, alice, td.ID,
					TodoPatch{Text: sp(texts[k]), When: when(dues[k], "", true)})
			}()
		}
		close(start)
		wg.Wait()

		win := -1
		for k, err := range errs {
			switch {
			case err == nil:
				if win != -1 {
					t.Fatalf("round %d: both saves wrote", i)
				}
				win = k
			case !errors.Is(err, ErrStaleDraft):
				t.Fatalf("round %d: handle %d: %v", i, k, err)
			}
		}
		if win == -1 {
			t.Fatalf("round %d: no save wrote: %v", i, errs)
		}
		if got := e.get(alice, td.ID).Text; got != texts[win] {
			t.Fatalf("round %d: text %q, want the winner's %q", i, got, texts[win])
		}
		due := e.byKind(e.pending(td.ID), model.ReminderDue)
		if len(due) != 1 || !due[0].FireAt.Equal(*dues[win]) {
			t.Fatalf("round %d: due reminders %+v, want one at %v", i, due, dues[win])
		}
	}
}

func TestEditClaimedReminderIsCancelledAndNotAdvancedTwice(t *testing.T) {
	e := newEnv(t)
	bst, b := e.node("B")
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	rem := e.claimOne(b, at(18, 1).UTC())

	// The edit moves the item to 20:00 while the scheduler holds the claim.
	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(at(20, 0), "daily 20:00", true)})
	e.must(err)
	if !got.DueAt.Equal(*at(20, 0)) {
		t.Fatalf("got %+v", got)
	}
	ok, err := bst.MarkReminderSent(ctx, rem.ID, 1, e.clk.Now())
	e.must(err)
	if ok {
		t.Fatal("MarkReminderSent succeeded for a cancelled reminder")
	}
	e.must(b.ReminderSent(ctx, rem.ID, 1, e.clk.Now()))
	st := e.get(alice, td.ID)
	if st.DueAt == nil || !st.DueAt.Equal(*at(20, 0)) || st.Recur != "daily 20:00" {
		t.Fatalf("advanced: %+v", st)
	}
	ps := e.pending(td.ID)
	if len(ps) != 1 || !ps[0].FireAt.Equal(*at(20, 0)) {
		t.Fatalf("reminders %+v", ps)
	}
}

func TestEditKeepsSchedulerAdvanceWhenWhenIsNil(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	rem := e.claimOne(e.svc, at(18, 1).UTC())
	e.must(e.svc.ReminderSent(ctx, rem.ID, 1, e.clk.Now()))
	next := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)

	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{Text: sp("pills!")})
	e.must(err)
	if got.DueAt == nil || !got.DueAt.Equal(next) {
		t.Fatalf("got %+v", got)
	}
	ps := e.pending(td.ID)
	if len(ps) != 1 || !ps[0].FireAt.Equal(next) {
		t.Fatalf("reminders %+v", ps)
	}
}

func TestEditTZChangesOnlyWithWhen(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "milk", at(15, 0), nil)
	e.must(e.svc.SetTZ(ctx, alice.UserID, "Asia/Tehran"))
	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{Text: sp("oat")})
	e.must(err)
	if got.TZ != td.TZ || got.TZ == "Asia/Tehran" {
		t.Fatalf("text edit changed TZ: %q (was %q)", got.TZ, td.TZ)
	}
	got, err = e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(at(18, 0), "", true)})
	e.must(err)
	if got.TZ != "Asia/Tehran" || e.get(alice, td.ID).TZ != "Asia/Tehran" || got.Num != td.Num {
		t.Fatalf("got %+v", got)
	}
}

// schedCase is one When that AddTodo and UpdateTodo must turn into the same
// schedule. The want values pin the schedule that both paths give.
type schedCase struct {
	name    string
	tz      string
	now     time.Time
	due     *time.Time // the parsed due time
	hasTime bool
	rule    string
	wantDue *time.Time // nil: no due time
	wantRem bool       // one pending 'due' reminder at wantDue
}

func schedCases(t *testing.T) []schedCase {
	t.Helper()
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	tehran, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	in := func(loc *time.Location, y int, m time.Month, d, h, mi int) *time.Time {
		v := time.Date(y, m, d, h, mi, 0, 0, loc).UTC()
		return &v
	}
	utc := func(y int, m time.Month, d, h, mi int) *time.Time { return in(time.UTC, y, m, d, h, mi) }
	// Summer time in London ends on 2026-10-25 at 01:00 UTC.
	beforeDST := *utc(2026, 10, 24, 12, 0)
	return []schedCase{
		{name: "no date", tz: "UTC", now: t0},
		{name: "once future", tz: "UTC", now: t0, due: at(18, 0), hasTime: true,
			wantDue: at(18, 0), wantRem: true},
		{name: "once past", tz: "UTC", now: t0, due: at(9, 0), hasTime: true, wantDue: at(9, 0)},
		{name: "date-only sentinel", tz: "UTC", now: t0, due: at(23, 59), wantDue: at(23, 59)},
		{name: "date-only sentinel Tehran", tz: "Asia/Tehran", now: t0,
			due: in(tehran, 2026, 10, 10, 23, 59), wantDue: in(tehran, 2026, 10, 10, 23, 59)},
		{name: "date-only tomorrow Tehran", tz: "Asia/Tehran", now: t0,
			due: in(tehran, 2026, 10, 11, 9, 0), wantDue: in(tehran, 2026, 10, 11, 9, 0), wantRem: true},
		{name: "London once across DST", tz: "Europe/London", now: beforeDST,
			due: in(london, 2026, 10, 26, 9, 0), hasTime: true,
			wantDue: utc(2026, 10, 26, 9, 0), wantRem: true},
		{name: "London daily across DST", tz: "Europe/London", now: beforeDST, rule: "daily 09:00",
			wantDue: utc(2026, 10, 25, 9, 0), wantRem: true},
		{name: "London daily before DST ends", tz: "Europe/London", now: *utc(2026, 10, 24, 7, 0),
			rule: "daily 09:00", wantDue: utc(2026, 10, 24, 8, 0), wantRem: true},
		{name: "Tehran daily", tz: "Asia/Tehran", now: t0, rule: "daily 09:00",
			wantDue: in(tehran, 2026, 10, 11, 9, 0), wantRem: true},
		{name: "Tehran rule with future due", tz: "Asia/Tehran", now: t0, rule: "daily 20:00",
			due: in(tehran, 2026, 10, 10, 20, 0), hasTime: true,
			wantDue: in(tehran, 2026, 10, 10, 20, 0), wantRem: true},
		{name: "London every 3d across DST", tz: "Europe/London", now: beforeDST,
			rule: "every 3d 2026-10-22 09:00", wantDue: utc(2026, 10, 25, 9, 0), wantRem: true},
		{name: "every 3d with past due", tz: "UTC", now: t0, rule: "every 3d 2026-10-10 09:00",
			due: at(9, 0), hasTime: true, wantDue: utc(2026, 10, 13, 9, 0), wantRem: true},
		{name: "London monthly last Friday", tz: "Europe/London", now: t0, rule: "monthly fri#last 16:00",
			wantDue: utc(2026, 10, 30, 16, 0), wantRem: true},
		// 2026-10-10 is 18 Mehr 1405. Mehr has 30 days, so the 31st clamps
		// to 30 Mehr, which is 2026-10-22.
		{name: "Tehran Jalali monthly 31", tz: "Asia/Tehran", now: t0, rule: "monthly 31 10:00 jalali",
			wantDue: in(tehran, 2026, 10, 22, 10, 0), wantRem: true},
		// 29 Ordibehesht 1405 is in the past; 29 Ordibehesht 1406 is 2027-05-19.
		{name: "Tehran Jalali yearly 02-29", tz: "Asia/Tehran", now: t0, rule: "yearly 02-29 09:00 jalali",
			wantDue: in(tehran, 2027, 5, 19, 9, 0), wantRem: true},
		// Feb 29 clamps to Feb 28 in a common year.
		{name: "Gregorian yearly 02-29", tz: "UTC", now: t0, rule: "yearly 02-29 09:00",
			wantDue: utc(2027, 2, 28, 9, 0), wantRem: true},
	}
}

// checkSched compares a returned todo, its stored row and its pending
// reminders with the case.
func (e *env) checkSched(path string, c schedCase, td model.Todo) {
	e.t.Helper()
	sameDue := func(a, b *time.Time) bool { return (a == nil) == (b == nil) && (a == nil || a.Equal(*b)) }
	if td.TZ != c.tz || td.Recur != c.rule || !sameDue(td.DueAt, c.wantDue) {
		e.t.Fatalf("%s: tz %q recur %q due %v, want %q %q %v", path, td.TZ, td.Recur, td.DueAt, c.tz, c.rule, c.wantDue)
	}
	if st := e.get(alice, td.ID); st.TZ != td.TZ || st.Recur != td.Recur || !sameDue(st.DueAt, td.DueAt) {
		e.t.Fatalf("%s: stored %+v, returned %+v", path, st, td)
	}
	ps := e.pending(td.ID)
	if !c.wantRem && len(ps) != 0 {
		e.t.Fatalf("%s: reminders %+v, want none", path, ps)
	}
	if c.wantRem && (len(ps) != 1 || ps[0].Kind != model.ReminderDue || !ps[0].FireAt.Equal(*c.wantDue)) {
		e.t.Fatalf("%s: reminders %+v, want one 'due' at %v", path, ps, c.wantDue)
	}
}

// TestAddAndEditGiveTheSameSchedule runs AddTodo and UpdateTodo with the
// same Result. Both must give the same due time, rule, zone and reminder.
func TestAddAndEditGiveTheSameSchedule(t *testing.T) {
	for _, c := range schedCases(t) {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			// The edited item has another schedule, made in another zone.
			e.must(e.svc.SetTZ(ctx, alice.UserID, "America/New_York"))
			oldDue := c.now.Add(2 * time.Hour)
			old := e.add(alice, "old", &oldDue, nil)
			e.clk.Set(c.now)
			e.must(e.svc.SetTZ(ctx, alice.UserID, c.tz))

			r := *when(c.due, c.rule, c.hasTime)
			r.Text = "new"
			added, err := e.svc.AddTodo(ctx, alice, r)
			e.must(err)
			e.checkSched("add", c, added)

			edited, err := e.svc.UpdateTodo(ctx, alice, old.ID, TodoPatch{When: when(c.due, c.rule, c.hasTime)})
			e.must(err)
			e.checkSched("edit", c, edited)
		})
	}
}

func TestSaveEditDraftWakesOnlyAfterACommit(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "milk", nil, nil)
	w := e.waker.n.Load()

	// A stale draft: nothing is written and nothing wakes.
	d := e.startEdit(alice, 5)
	stale := d
	stale.Version--
	_, err := e.svc.SaveEditDraft(ctx, stale, alice, td.ID, TodoPatch{When: when(at(18, 0), "", true)})
	wantErr(t, "stale", err, ErrStaleDraft)
	if e.waker.n.Load() != w {
		t.Fatal("wake after a stale draft")
	}
	// The draft is deleted, then the update fails: the delete rolls back.
	_, err = e.svc.SaveEditDraft(ctx, d, alice, td.ID, TodoPatch{Text: sp(" "), When: when(at(18, 0), "", true)})
	wantErr(t, "rollback", err, ErrEmptyText)
	if e.waker.n.Load() != w {
		t.Fatal("wake after a rollback")
	}
	if kept, err := e.svc.Draft(ctx, alice.ChatID, alice.UserID); err != nil || kept.Version != d.Version {
		t.Fatalf("draft after rollback: %+v %v", kept, err)
	}
	if st := e.get(alice, td.ID); st.DueAt != nil || len(e.pending(td.ID)) != 0 {
		t.Fatalf("rollback wrote: %+v", st)
	}
	// A save with a When commits and wakes the scheduler.
	_, err = e.svc.SaveEditDraft(ctx, d, alice, td.ID, TodoPatch{When: when(at(18, 0), "", true)})
	e.must(err)
	if e.waker.n.Load() == w {
		t.Fatal("no wake after a save with a When")
	}
	if due := e.byKind(e.pending(td.ID), model.ReminderDue); len(due) != 1 || !due[0].FireAt.Equal(*at(18, 0)) {
		t.Fatalf("reminders %+v", due)
	}
}

func TestEditWhenByAssigneeUsesAssigneeTZ(t *testing.T) {
	e := newEnv(t)
	td := e.assigned(at(15, 0), "")
	e.must(e.svc.SetTZ(ctx, bob.UserID, "Asia/Tehran"))
	if td.TZ == "Asia/Tehran" {
		t.Fatalf("creator TZ is already %q", td.TZ)
	}
	got, err := e.svc.UpdateTodo(ctx, bob, td.ID, TodoPatch{When: when(nil, "daily 09:00", false)})
	e.must(err)
	// 09:00 in Tehran (UTC+03:30) is 05:30 UTC; 09:00 today has passed.
	want := time.Date(2026, 10, 11, 5, 30, 0, 0, time.UTC)
	if got.TZ != "Asia/Tehran" || got.Recur != "daily 09:00" || got.DueAt == nil || !got.DueAt.Equal(want) {
		t.Fatalf("got %+v, want TZ Asia/Tehran and due %v", got, want)
	}
	if st := e.get(aliceG, td.ID); st.TZ != "Asia/Tehran" {
		t.Fatalf("stored TZ %q", st.TZ)
	}
	if ps := e.pending(td.ID); len(ps) != 1 || !ps[0].FireAt.Equal(want) {
		t.Fatalf("reminders %+v", ps)
	}
}

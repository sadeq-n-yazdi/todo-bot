package service

import (
	"errors"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

var (
	stranger = Actor{UserID: 50, ChatID: 50, ChatKind: model.ChatPrivate}
	farStep  int
)

func sp(s string) *string { return &s }
func ip(i int64) *int64   { return &i }

// pending lists the pending reminders of an item. It claims them far in the
// future, so call it once per moment you want to look.
func (e *env) pending(todoID int64) []model.Reminder {
	e.t.Helper()
	farStep++
	far := t0.AddDate(10, 0, 0).Add(time.Duration(farStep) * time.Hour)
	var out []model.Reminder
	for _, r := range e.claim(e.svc, far) {
		if r.TodoID == todoID {
			out = append(out, r.Reminder)
		}
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

func TestSaveEditDraftTwoHandlesOneWrite(t *testing.T) {
	e := newEnv(t)
	_, b := e.node("B")
	td := e.add(alice, "milk", nil, nil)
	d := e.startEdit(alice, 5)
	if _, err := e.svc.SaveEditDraft(ctx, d, alice, td.ID, TodoPatch{Text: sp("first")}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.SaveEditDraft(ctx, d, alice, td.ID, TodoPatch{Text: sp("second")}); !errors.Is(err, ErrStaleDraft) {
		t.Fatalf("second: %v", err)
	}
	if e.get(alice, td.ID).Text != "first" {
		t.Fatal("second save wrote")
	}
}

func TestEditClaimedReminderIsCancelledAndNotAdvancedTwice(t *testing.T) {
	e := newEnv(t)
	td := e.add(alice, "pills", at(18, 0), mustRule(t, "daily 18:00"))
	rem := e.claimOne(e.svc, at(18, 1).UTC())

	// The edit moves the item to 20:00 while the scheduler holds the claim.
	got, err := e.svc.UpdateTodo(ctx, alice, td.ID, TodoPatch{When: when(at(20, 0), "daily 20:00", true)})
	e.must(err)
	if !got.DueAt.Equal(*at(20, 0)) {
		t.Fatalf("got %+v", got)
	}
	ok, err := e.st.MarkReminderSent(ctx, rem.ID, 1, e.clk.Now())
	e.must(err)
	if ok {
		t.Fatal("MarkReminderSent succeeded for a cancelled reminder")
	}
	e.must(e.svc.ReminderSent(ctx, rem.ID, 1, e.clk.Now()))
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

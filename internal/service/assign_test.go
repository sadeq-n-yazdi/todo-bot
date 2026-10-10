package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

var (
	groupChat = int64(-100)
	// aliceG is alice speaking in the family group; bob and carol are
	// other users in their own private chats.
	aliceG = Actor{UserID: 7, ChatID: groupChat, ChatKind: model.ChatGroup}
	bob    = Actor{UserID: 8, ChatID: 8, ChatKind: model.ChatPrivate}
	carol  = Actor{UserID: 9, ChatID: 9, ChatKind: model.ChatPrivate}
)

func (e *env) username(id int64) string {
	e.t.Helper()
	u, err := e.st.GetUser(ctx, id)
	e.must(err)
	return u.Username
}

func (e *env) addFor(a Actor, assignee int64, text string, due *time.Time, rule string) model.Todo {
	e.t.Helper()
	r := nlparse.Result{Text: text, Due: due, HasTime: due != nil}
	if rule != "" {
		r.Rule = mustRule(e.t, rule)
	}
	td, err := e.svc.AddTodoFor(ctx, a, assignee, r)
	e.must(err)
	return td
}

// assigned returns a todo created by alice in the group for bob.
func (e *env) assigned(due *time.Time, rule string) model.Todo {
	e.t.Helper()
	e.must(e.svc.NoteUsername(ctx, 8, "Bob_X"))
	return e.addFor(aliceG, 8, "pick up kid", due, rule)
}

func wantErr(t *testing.T, what string, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("%s: err = %v, want %v", what, got, want)
	}
}

func TestNoteUsernameStoresLowercaseAndCreatesUser(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.NoteUsername(ctx, 8, "@Bob_X"))
	if got := e.username(8); got != "bob_x" {
		t.Fatalf("username = %q", got)
	}
	u, err := e.svc.UserByUsername(ctx, "@BOB_x")
	e.must(err)
	if u.ID != 8 {
		t.Fatalf("resolved %d", u.ID)
	}
	_, err = e.svc.UserByUsername(ctx, "nobody")
	wantErr(t, "unknown", err, ErrNotFound)
	_, err = e.svc.UserByUsername(ctx, "")
	wantErr(t, "empty", err, ErrNotFound)
}

func TestNoteUsernameWritesOnlyOnChange(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.NoteUsername(ctx, 8, "bob_x"))
	// Another account takes the name over behind NoteUsername's back (as
	// another node would). If NoteUsername used a cache or wrote
	// unconditionally for user 8 it would steal the name back.
	e.must(e.svc.NoteUsername(ctx, 9, "bob_x"))
	e.must(e.svc.NoteUsername(ctx, 8, "bob_x")) // 8 stored "" -> differs -> reclaims
	if e.username(8) != "bob_x" || e.username(9) != "" {
		t.Fatalf("after reclaim: 8=%q 9=%q", e.username(8), e.username(9))
	}
	// Unchanged name: no write. Prove it by checking that the row is not
	// touched: give the name to 9, then note 9's current name again; it
	// must stay with 9 and 8 must stay empty.
	e.must(e.svc.NoteUsername(ctx, 9, "bob_x"))
	e.must(e.svc.NoteUsername(ctx, 9, "Bob_X"))
	if e.username(9) != "bob_x" || e.username(8) != "" {
		t.Fatalf("no-op changed state: 8=%q 9=%q", e.username(8), e.username(9))
	}
}

func TestNoteUsernameEmptyClears(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.NoteUsername(ctx, 8, "bob_x"))
	e.must(e.svc.NoteUsername(ctx, 8, ""))
	if got := e.username(8); got != "" {
		t.Fatalf("username = %q", got)
	}
	_, err := e.svc.UserByUsername(ctx, "bob_x")
	wantErr(t, "cleared", err, ErrNotFound)
}

func TestNoteUsernameMovesToNewAccount(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.NoteUsername(ctx, 8, "bob_x"))
	e.must(e.svc.NoteUsername(ctx, 9, "bob_x"))
	if e.username(8) != "" || e.username(9) != "bob_x" {
		t.Fatalf("8=%q 9=%q", e.username(8), e.username(9))
	}
	u, err := e.svc.UserByUsername(ctx, "bob_x")
	e.must(err)
	if u.ID != 9 {
		t.Fatalf("resolves to %d", u.ID)
	}
}

func TestNoteUsernameNeedsNoPriorUser(t *testing.T) {
	e := newEnv(t)
	// The user row does not exist yet: NoteUsername creates it.
	e.must(e.svc.NoteUsername(ctx, 42, ""))
	if _, err := e.st.GetUser(ctx, 42); err != nil {
		t.Fatal(err)
	}
}

func TestAddTodoForFillsAssignee(t *testing.T) {
	e := newEnv(t)
	td := e.assigned(at(15, 0), "")
	if td.AssigneeID != 8 || td.AssigneeName != "bob_x" || td.ChatID != groupChat || td.CreatedBy != 7 {
		t.Fatalf("todo = %+v", td)
	}
	got, err := e.svc.Todo(ctx, aliceG, td.ID)
	e.must(err)
	if got.AssigneeID != 8 || got.AssigneeName != "bob_x" {
		t.Fatalf("stored = %+v", got)
	}
	// The reminder stays on the creating chat; routing is the scheduler's.
	e.clk.Set(at(15, 1).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	if len(rs) != 1 || rs[0].ChatID != groupChat || rs[0].Todo.AssigneeID != 8 || rs[0].Todo.AssigneeName != "bob_x" {
		t.Fatalf("claimed = %+v", rs)
	}
}

func TestAddTodoForAssigneeWithoutUsername(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Prefs(ctx, 8, "") // bob exists, name unknown
	e.must(err)
	td := e.addFor(aliceG, 8, "x", nil, "")
	if td.AssigneeID != 8 || td.AssigneeName != "" {
		t.Fatalf("todo = %+v", td)
	}
}

func TestAddTodoForOwnPrivateChatStoresNoAssignee(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.NoteUsername(ctx, 8, "bob_x"))
	td := e.addFor(bob, 8, "mine", nil, "")
	if td.AssigneeID != 0 || td.AssigneeName != "" {
		t.Fatalf("todo = %+v", td)
	}
	got, err := e.svc.Todo(ctx, bob, td.ID)
	e.must(err)
	if got.AssigneeID != 0 {
		t.Fatalf("stored assignee %d", got.AssigneeID)
	}
}

func TestAddTodoForSelfFromGroupIsStored(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.NoteUsername(ctx, 7, "alice_a"))
	td := e.addFor(aliceG, 7, "remind me privately", nil, "")
	if td.AssigneeID != 7 || td.AssigneeName != "alice_a" {
		t.Fatalf("todo = %+v", td)
	}
}

func TestAddTodoForZeroIsPlainAdd(t *testing.T) {
	e := newEnv(t)
	td := e.addFor(aliceG, 0, "plain", nil, "")
	if td.AssigneeID != 0 || td.AssigneeName != "" {
		t.Fatalf("todo = %+v", td)
	}
}

func TestAddTodoForUnknownAssignee(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.AddTodoFor(ctx, aliceG, 999, nlparse.Result{Text: "x"})
	wantErr(t, "unknown assignee", err, ErrNotFound)
	// nothing was created
	if ts, _, err := e.svc.List(ctx, groupChat, 0); err != nil || len(ts) != 0 {
		t.Fatalf("list = %v, %v", ts, err)
	}
}

func TestAddTodoForKeepsAddTodoValidation(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.NoteUsername(ctx, 8, "bob_x"))
	_, err := e.svc.AddTodoFor(ctx, aliceG, 8, nlparse.Result{Text: "  "})
	wantErr(t, "empty", err, ErrEmptyText)
}

func TestListAssigned(t *testing.T) {
	e := newEnv(t)
	d := e.assigned(at(15, 0), "")
	later := e.addFor(aliceG, 8, "later", at(20, 0), "")
	undated := e.addFor(aliceG, 8, "undated", nil, "")
	own := e.addFor(bob, 8, "own chat", at(13, 0), "") // no assignee stored
	_ = own
	e.addFor(aliceG, 0, "not bob's", nil, "")
	done := e.addFor(aliceG, 8, "done one", at(14, 0), "")
	_, err := e.svc.Complete(ctx, aliceG, done.ID)
	e.must(err)

	got, err := e.svc.ListAssigned(ctx, 8)
	e.must(err)
	if len(got) != 3 || got[0].ID != d.ID || got[1].ID != later.ID || got[2].ID != undated.ID {
		t.Fatalf("list = %+v", got)
	}
	if got[0].AssigneeName != "bob_x" {
		t.Fatalf("assignee name = %q", got[0].AssigneeName)
	}
	other, err := e.svc.ListAssigned(ctx, 9)
	e.must(err)
	if len(other) != 0 {
		t.Fatalf("carol sees %+v", other)
	}
}

func TestListAssignedExcludesOwnPrivateChat(t *testing.T) {
	e := newEnv(t)
	e.must(e.svc.NoteUsername(ctx, 8, "bob_x"))
	e.must(e.svc.NoteUsername(ctx, 9, "carol_c"))
	// carol assigns to bob from carol's private chat: lives in chat 9.
	inCarol := e.addFor(carol, 8, "from carol", nil, "")
	// A row assigned to bob that lives in bob's own chat (created by hand,
	// as an older bot or direct DB edit could): must be filtered out.
	own := model.Todo{ChatID: 8, Text: "in bob's chat", Status: model.TodoOpen, TZ: "UTC",
		CreatedBy: 9, CreatedAt: t0, UpdatedAt: t0, AssigneeID: 8}
	e.must(e.st.EnsureChat(ctx, model.Chat{ID: 8, Kind: model.ChatPrivate, CreatedAt: t0}))
	e.must(e.st.InsertTodo(ctx, &own))
	got, err := e.svc.ListAssigned(ctx, 8)
	e.must(err)
	if len(got) != 1 || got[0].ID != inCarol.ID {
		t.Fatalf("list = %+v", got)
	}
}

// TestAuthorisationEitherOr covers every action: the creating chat and the
// assignee may act, a bystander may not.
func TestAuthorisationEitherOr(t *testing.T) {
	type action struct {
		name string
		do   func(e *env, a Actor, td model.Todo, rem model.Reminder) error
	}
	actions := []action{
		{"Todo", func(e *env, a Actor, td model.Todo, _ model.Reminder) error {
			_, err := e.svc.Todo(ctx, a, td.ID)
			return err
		}},
		{"Edit", func(e *env, a Actor, td model.Todo, _ model.Reminder) error {
			_, err := e.svc.Edit(ctx, a, td.ID, "edited")
			return err
		}},
		{"Remind", func(e *env, a Actor, td model.Todo, _ model.Reminder) error {
			_, err := e.svc.Remind(ctx, a, td.ID, e.clk.Now().Add(time.Hour))
			return err
		}},
		{"Snooze", func(e *env, a Actor, _ model.Todo, rem model.Reminder) error {
			_, err := e.svc.Snooze(ctx, a, rem.ID, "10m")
			return err
		}},
		{"AckReminder", func(e *env, a Actor, _ model.Todo, rem model.Reminder) error {
			_, err := e.svc.AckReminder(ctx, a, rem.ID)
			return err
		}},
		{"Complete", func(e *env, a Actor, td model.Todo, _ model.Reminder) error {
			_, err := e.svc.Complete(ctx, a, td.ID)
			return err
		}},
		{"Delete", func(e *env, a Actor, td model.Todo, _ model.Reminder) error {
			return e.svc.Delete(ctx, a, td.ID)
		}},
	}
	actors := []struct {
		name  string
		a     Actor
		allow bool
	}{
		{"creator in group", aliceG, true},
		{"assignee in own DM", bob, true},
		{"bystander", carol, false},
		{"creator in her DM", alice, false}, // wrong chat, not the assignee
	}
	for _, act := range actions {
		for _, who := range actors {
			t.Run(act.name+"/"+who.name, func(t *testing.T) {
				e := newEnv(t)
				td := e.assigned(at(15, 0), "")
				e.clk.Set(at(15, 1).UTC())
				rs := e.claim(e.svc, e.clk.Now())
				if len(rs) != 1 {
					t.Fatalf("claimed %d", len(rs))
				}
				err := act.do(e, who.a, td, rs[0].Reminder)
				if who.allow && err != nil {
					t.Fatalf("denied: %v", err)
				}
				if !who.allow {
					wantErr(t, "foreign", err, ErrForeignChat)
				}
			})
		}
	}
}

func TestUnassignedItemStaysChatOnly(t *testing.T) {
	e := newEnv(t)
	td := e.addFor(aliceG, 0, "plain", at(15, 0), "")
	_, err := e.svc.Todo(ctx, bob, td.ID)
	wantErr(t, "bob", err, ErrForeignChat)
	// A user whose id equals nobody's assignee must not match AssigneeID 0.
	zero := Actor{UserID: 0, ChatID: 5}
	_, err = e.svc.Todo(ctx, zero, td.ID)
	wantErr(t, "zero user", err, ErrForeignChat)
}

func TestAssigneeSnoozeFromDMReminder(t *testing.T) {
	e := newEnv(t)
	td := e.assigned(at(15, 0), "")
	e.clk.Set(at(15, 1).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 5, e.clk.Now()))
	r, err := e.svc.Snooze(ctx, bob, rs[0].ID, "10m")
	e.must(err)
	// The snooze stays on the creating chat and carries the assignee, so
	// the scheduler delivers it to bob.
	if r.ChatID != groupChat || r.TodoID != td.ID {
		t.Fatalf("snooze = %+v", r)
	}
	e.clk.Set(e.clk.Now().Add(11 * time.Minute))
	got := e.claim(e.svc, e.clk.Now())
	if len(got) != 1 || got[0].Kind != model.ReminderSnooze || got[0].Todo.AssigneeID != 8 {
		t.Fatalf("claimed = %+v", got)
	}
}

func TestAssigneeAckCompletesAndCreatorSeesIt(t *testing.T) {
	e := newEnv(t)
	td := e.assigned(at(15, 0), "")
	e.clk.Set(at(15, 1).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	done, err := e.svc.AckReminder(ctx, bob, rs[0].ID)
	e.must(err)
	if done.Status != model.TodoDone || done.ID != td.ID || done.ChatID != groupChat || done.AssigneeName != "bob_x" {
		t.Fatalf("done = %+v", done)
	}
	got, err := e.svc.Todo(ctx, aliceG, td.ID)
	e.must(err)
	if got.Status != model.TodoDone {
		t.Fatalf("creator sees %v", got.Status)
	}
	// Done items leave "Assigned to me".
	l, err := e.svc.ListAssigned(ctx, 8)
	e.must(err)
	if len(l) != 0 {
		t.Fatalf("assigned = %+v", l)
	}
}

func TestAssigneeDeleteIsHonouredByCreatorChat(t *testing.T) {
	e := newEnv(t)
	td := e.assigned(nil, "")
	e.must(e.svc.Delete(ctx, bob, td.ID))
	_, err := e.svc.Todo(ctx, aliceG, td.ID)
	wantErr(t, "deleted", err, ErrNotFound)
}

func TestRecurringAssignedKeepsAssignee(t *testing.T) {
	e := newEnv(t)
	td := e.assigned(at(18, 0), "daily 18:00")

	// ReminderSent advances the rule.
	e.clk.Set(at(18, 1).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 1, e.clk.Now()))
	got, err := e.svc.Todo(ctx, bob, td.ID)
	e.must(err)
	next := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	if got.AssigneeID != 8 || got.AssigneeName != "bob_x" || got.DueAt == nil || !got.DueAt.Equal(next) {
		t.Fatalf("after ReminderSent = %+v", got)
	}

	// Complete by the assignee, within the 12h window, advances again.
	e.clk.Set(next.Add(-time.Hour))
	got, err = e.svc.Complete(ctx, bob, td.ID)
	e.must(err)
	next2 := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	if got.AssigneeID != 8 || got.DueAt == nil || !got.DueAt.Equal(next2) {
		t.Fatalf("after Complete = %+v", got)
	}
	again, err := e.svc.Todo(ctx, aliceG, td.ID)
	e.must(err)
	if again.AssigneeID != 8 {
		t.Fatalf("stored assignee lost: %+v", again)
	}

	// SetTZ by the creator re-anchors her recurring items; assignee stays.
	e.must(e.svc.SetTZ(ctx, 7, "Asia/Tehran"))
	again, err = e.svc.Todo(ctx, aliceG, td.ID)
	e.must(err)
	if again.AssigneeID != 8 || again.AssigneeName != "bob_x" || again.TZ != "Asia/Tehran" {
		t.Fatalf("after SetTZ = %+v", again)
	}
}

func TestAssigneeAckOnRecurringLeavesItAlone(t *testing.T) {
	e := newEnv(t)
	td := e.assigned(at(18, 0), "daily 18:00")
	e.clk.Set(at(18, 1).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 1, e.clk.Now()))
	got, err := e.svc.AckReminder(ctx, bob, rs[0].ID)
	e.must(err)
	if got.Status != model.TodoOpen || got.AssigneeID != 8 || got.ID != td.ID {
		t.Fatalf("ack = %+v", got)
	}
}

// spyStore counts SetUsername calls that reach the pool (calls made inside
// WithTx go to the transaction's own store, not through the spy).
type spyStore struct {
	store.Store
	setUsername int
}

func (s *spyStore) SetUsername(ctx context.Context, id int64, name string) error {
	s.setUsername++
	return s.Store.SetUsername(ctx, id, name)
}

func TestNoteUsernameCallsSetUsernameOnlyOnChange(t *testing.T) {
	e := newEnv(t)
	spy := &spyStore{Store: e.st}
	svc := New(spy, e.clk, Config{NodeID: "S", DefaultHour: 9})
	svc.SetLogger(quiet())

	e.must(svc.NoteUsername(ctx, 8, "Bob_X"))
	if spy.setUsername != 1 {
		t.Fatalf("first note: %d writes", spy.setUsername)
	}
	e.must(svc.NoteUsername(ctx, 8, "bob_x"))
	e.must(svc.NoteUsername(ctx, 8, "@BOB_X"))
	if spy.setUsername != 1 {
		t.Fatalf("unchanged name wrote: %d writes", spy.setUsername)
	}
	e.must(svc.NoteUsername(ctx, 8, "bobby"))
	e.must(svc.NoteUsername(ctx, 8, ""))
	e.must(svc.NoteUsername(ctx, 8, ""))
	if spy.setUsername != 3 {
		t.Fatalf("writes = %d, want 3", spy.setUsername)
	}
}

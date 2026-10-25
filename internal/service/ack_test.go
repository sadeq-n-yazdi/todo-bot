package service

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// firedReminder adds an item due at 13:00, fires its reminder at 13:01 and
// returns the item and the sent reminder.
func (e *env) firedReminder(rule *recur.Rule) (model.Todo, model.Reminder) {
	e.t.Helper()
	td := e.add(alice, "pills", at(13, 0), rule)
	e.clk.Set(at(13, 1).UTC())
	rs := e.claim(e.svc, e.clk.Now())
	if len(rs) != 1 {
		e.t.Fatalf("claimed %d reminders, want 1", len(rs))
	}
	e.must(e.svc.ReminderSent(ctx, rs[0].ID, 55, e.clk.Now()))
	return td, rs[0].Reminder
}

func (e *env) reminderState(id int64) model.ReminderState {
	e.t.Helper()
	r, err := e.st.GetReminder(ctx, id)
	e.must(err)
	return r.State
}

// AckReminder reports whether this call changed the item (#157). A press
// that changes nothing also cancels nothing and does not wake the
// scheduler.
func TestAckReminderReportsChanged(t *testing.T) {
	t.Run("once", func(t *testing.T) {
		e := newEnv(t)
		_, rem := e.firedReminder(nil)
		got, changed, err := e.svc.AckReminder(ctx, alice, rem.ID)
		e.must(err)
		if !changed || got.Status != model.TodoDone {
			t.Fatalf("first: changed=%v %+v", changed, got)
		}
		wakes := e.waker.n.Load()
		got, changed, err = e.svc.AckReminder(ctx, alice, rem.ID)
		e.must(err)
		if changed || got.Status != model.TodoDone {
			t.Fatalf("second: changed=%v %+v", changed, got)
		}
		if n := e.waker.n.Load(); n != wakes {
			t.Fatalf("second ack woke the scheduler (%d -> %d)", wakes, n)
		}
	})
	t.Run("repeating", func(t *testing.T) {
		e := newEnv(t)
		_, rem := e.firedReminder(mustRule(t, "daily 13:00"))
		before, err := e.svc.Snooze(ctx, alice, rem.ID, ui.Snooze10m)
		e.must(err)
		got, changed, err := e.svc.AckReminder(ctx, alice, rem.ID)
		e.must(err)
		next := time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC)
		if !changed || got.Status != model.TodoOpen || got.DueAt == nil || !got.DueAt.Equal(next) {
			t.Fatalf("first: changed=%v %+v", changed, got)
		}
		if s := e.reminderState(before.ID); s != model.ReminderCancelled {
			t.Fatalf("snooze before the ack = %s, want cancelled", s)
		}
		// A snooze made after the ack is not cancelled by a second press.
		after, err := e.svc.Snooze(ctx, alice, rem.ID, ui.Snooze1h)
		e.must(err)
		wakes := e.waker.n.Load()
		got, changed, err = e.svc.AckReminder(ctx, alice, rem.ID)
		e.must(err)
		if changed || got.Status != model.TodoOpen || !got.DueAt.Equal(next) {
			t.Fatalf("second: changed=%v %+v", changed, got)
		}
		if s := e.reminderState(after.ID); s != model.ReminderPending {
			t.Fatalf("snooze after the ack = %s, want pending", s)
		}
		if n := e.waker.n.Load(); n != wakes {
			t.Fatalf("second ack woke the scheduler (%d -> %d)", wakes, n)
		}
	})
}

// Six presses of Done on one reminder at the same time, on two nodes, give
// one change: a one-off item is done once, and a repeating item keeps the
// next occurrence that the scheduler gave it.
func TestAckReminderConcurrentChangesOnce(t *testing.T) {
	for _, c := range []struct {
		name string
		rule string
	}{
		{"once", ""},
		{"repeating", "daily 13:00"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			_, other := e.node("B")
			var rule *recur.Rule
			if c.rule != "" {
				rule = mustRule(t, c.rule)
			}
			td, rem := e.firedReminder(rule)
			svcs := []*Svc{e.svc, other, e.svc, other, e.svc, other}
			var changes atomic.Int32
			var wg sync.WaitGroup
			start := make(chan struct{})
			for _, s := range svcs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, changed, err := s.AckReminder(ctx, alice, rem.ID)
					if err != nil {
						t.Error(err)
					}
					if changed {
						changes.Add(1)
					}
				}()
			}
			close(start)
			wg.Wait()
			if n := changes.Load(); n != 1 {
				t.Fatalf("%d changes, want 1", n)
			}
			got, err := e.svc.Todo(ctx, alice, td.ID)
			e.must(err)
			if c.rule == "" {
				if got.Status != model.TodoDone {
					t.Fatalf("status = %s", got.Status)
				}
				return
			}
			want := time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC)
			if got.Status != model.TodoOpen || got.DueAt == nil || !got.DueAt.Equal(want) {
				t.Fatalf("repeating = %+v, want due %v", got, want)
			}
			// The next occurrence still fires.
			e.clk.Set(want)
			if rs := e.claim(e.svc, e.clk.Now()); len(rs) != 1 || !rs[0].FireAt.Equal(want) {
				t.Fatalf("next occurrence lost: %+v", rs)
			}
		})
	}
}

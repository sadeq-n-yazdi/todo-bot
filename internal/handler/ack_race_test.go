package handler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Several presses of Done on one reminder message at the same moment
// change the item one time and send one "did" notice (#157): for a one-off
// item and for a repeating item. Every press still edits the message. The
// handler runs on the real service and SQLite.
func TestReminderDoneRaceSendsOneNotice(t *testing.T) {
	for _, c := range []struct {
		name string
		rule string
	}{
		{"once", ""},
		{"repeating", "daily 13:00"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newWz(t)
			ctx := context.Background()
			due := utcT(2026, 10, 10, 13, 0)
			td := e.add(wzGrp, "fix the door", &due, c.rule, wzBob)
			// The reminder fires and goes to Bob's private chat.
			e.clk.Set(utcT(2026, 10, 10, 13, 1))
			rs, err := e.svc.ClaimReminders(ctx, e.clk.Now(), 10)
			if err != nil || len(rs) != 1 {
				t.Fatalf("claim = %+v, %v", rs, err)
			}
			if err := e.svc.ReminderSent(ctx, rs[0].ID, 7, e.clk.Now()); err != nil {
				t.Fatal(err)
			}
			data := ui.Encode(ui.Action{Kind: ui.ActReminderDone, ID: rs[0].ID})
			const n = 6
			var seq atomic.Int64
			seq.Store(5000)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for range n {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					err := e.h.Handle(ctx, tg.Update{ID: seq.Add(1), Callback: &tg.Callback{
						ID: "cb", FromID: wzBob, FromUsername: wzNames[wzBob], ChatID: wzBob, MessageID: 7, Data: data,
					}})
					if err != nil {
						t.Error(err)
					}
				}()
			}
			close(start)
			wg.Wait()
			if got := e.sendsTo(wzGrp); len(got) != 1 {
				t.Fatalf("%d notices, want 1: %q", len(got), got)
			}
			edits := 0
			for _, m := range e.fs.Messages() {
				if m.Edit && m.ChatID == wzBob && m.MsgID == 7 {
					edits++
				}
			}
			if edits != n {
				t.Fatalf("%d edits of the reminder, want %d", edits, n)
			}
			got := e.todoByID(wzGrp, td.ID)
			if c.rule == "" {
				if got.Status != model.TodoDone {
					t.Fatalf("status = %s", got.Status)
				}
				return
			}
			if want := utcT(2026, 10, 11, 13, 0); got.Status != model.TodoOpen || got.DueAt == nil || !got.DueAt.Equal(want) {
				t.Fatalf("repeating = %+v, want due %v", got, want)
			}
		})
	}
}

// lostAck completes the item for another node first, so the call of the
// handler does not change it.
type lostAck struct{ *fakeAssign }

func (l lostAck) AckReminder(ctx context.Context, a service.Actor, rid int64) (model.Todo, bool, error) {
	if _, _, err := l.fakeAssign.AckReminder(ctx, a, rid); err != nil {
		return model.Todo{}, false, err
	}
	return l.fakeAssign.AckReminder(ctx, a, rid)
}

// The Done button on a reminder sends no notice when AckReminder did not
// change the item, but it still edits the message.
func TestReminderDoneNoNoticeWhenNotChanged(t *testing.T) {
	e := newAssignEnv()
	e.h.Svc = lostAck{e.svc}
	e.svc.todos = append(e.svc.todos, &model.Todo{ID: 1, ChatID: grp, Num: 4, Text: "pick up son",
		Status: model.TodoOpen, CreatedBy: creator, AssigneeID: assignee, AssigneeName: "wifey"})
	e.svc.rems[40] = 1
	e.press(assignee, assignee, "rd:40")
	edits := 0
	for _, s := range e.fs.Messages() {
		if s.ChatID == grp {
			t.Errorf("notice sent: %q", s.Msg.Text)
		}
		if s.Edit && s.ChatID == assignee && s.MsgID == 9 {
			edits++
		}
	}
	if edits != 1 {
		t.Fatalf("edits = %d, want 1: %+v", edits, e.fs.Messages())
	}
	if e.svc.todos[0].Status != model.TodoDone {
		t.Fatalf("item not done: %+v", e.svc.todos[0])
	}
}

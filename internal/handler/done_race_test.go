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

// Several presses of Done at the same moment on one open item change it
// one time and send one "did" notice (#147): for a one-off item and for a
// repeating item. The handler runs on the real service and SQLite.
func TestDoneButtonRaceSendsOneNotice(t *testing.T) {
	for _, c := range []struct {
		name string
		rule string
	}{
		{"once", ""},
		{"repeating", "daily 13:00"}, // the next time is 25h later: out of the 12h window
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newWz(t)
			due := utcT(2026, 10, 10, 13, 0)
			td := e.add(wzGrp, "fix the door", &due, c.rule, wzBob)
			data := ui.Encode(ui.Action{Kind: ui.ActDone, ID: td.ID})
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
					err := e.h.Handle(context.Background(), tg.Update{ID: seq.Add(1), Callback: &tg.Callback{
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

// lostRace completes the item for another node first, so the call of the
// handler does not change it.
type lostRace struct{ *fakeAssign }

func (l lostRace) Complete(ctx context.Context, a service.Actor, id int64) (model.Todo, bool, error) {
	if _, _, err := l.fakeAssign.Complete(ctx, a, id); err != nil {
		return model.Todo{}, false, err
	}
	return l.fakeAssign.Complete(ctx, a, id)
}

// /done and the Done button send no notice when Complete did not change
// the item.
func TestDoneNoNoticeWhenNotChanged(t *testing.T) {
	e := newAssignEnv()
	e.h.Svc = lostRace{e.svc}
	e.svc.todos = append(e.svc.todos,
		&model.Todo{ID: 1, ChatID: grp, Num: 4, Text: "pick up son",
			Status: model.TodoOpen, CreatedBy: creator, AssigneeID: assignee, AssigneeName: "wifey"},
		&model.Todo{ID: 2, ChatID: grp, Num: 5, Text: "buy milk",
			Status: model.TodoOpen, CreatedBy: creator, AssigneeID: assignee, AssigneeName: "wifey"})
	e.msg(assignee, assignee, "/done 4")
	e.press(assignee, assignee, ui.Encode(ui.Action{Kind: ui.ActDone, ID: 2}))
	for _, s := range e.fs.Messages() {
		if s.ChatID == grp {
			t.Errorf("notice sent: %q", s.Msg.Text)
		}
	}
	if e.svc.todos[0].Status != model.TodoDone || e.svc.todos[1].Status != model.TodoDone {
		t.Fatalf("items not done: %+v %+v", e.svc.todos[0], e.svc.todos[1])
	}
}

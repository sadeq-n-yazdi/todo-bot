package web

import (
	"fmt"
	"sync"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
)

// Several Done requests at the same moment on one open item change it one
// time and send one "did" notice (#147): for a one-off item and for a
// repeating item.
func TestDoneRaceSendsOneNotice(t *testing.T) {
	for _, c := range []struct {
		name  string
		recur string
	}{
		{"once", ""},
		{"repeating", "weekly sun 14:00"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newWriteEnv(t)
			td := e.addTodo(model.Todo{ChatID: groupFamily, Text: "fix the door", DueAt: at(2026, 3, 1, 14, 0),
				Recur: c.recur, TZ: "UTC", AssigneeID: idAnn, CreatedBy: idBob})
			e.send.Reset()
			p := chatPath(idAnn, fmt.Sprintf("/items/%d/done", td.ID))
			const n = 6
			codes := make([]int, n)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range n {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					codes[i] = e.do("POST", p, `{}`, bearer(e.ann)).Code
				}()
			}
			close(start)
			wg.Wait()
			e.srv.codes.notices.Wait()
			for i, code := range codes {
				if code != 200 && code != 404 {
					t.Errorf("request %d: status %d", i, code)
				}
			}
			if msgs := e.send.Messages(); len(msgs) != 1 || msgs[0].ChatID != groupFamily {
				t.Fatalf("%d notices, want 1: %+v", len(msgs), msgs)
			}
			got := e.todo(td.ID)
			if c.recur == "" {
				if got.Status != model.TodoDone {
					t.Fatalf("status = %s", got.Status)
				}
				return
			}
			if want := at(2026, 3, 8, 14, 0); got.Status != model.TodoOpen || got.DueAt == nil || !got.DueAt.Equal(*want) {
				t.Fatalf("repeating = %+v, want due %v", got, want)
			}
		})
	}
}

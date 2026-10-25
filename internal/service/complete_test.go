package service

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
)

// Complete reports whether this call changed the item (#147).
func TestCompleteReportsChanged(t *testing.T) {
	e := newEnv(t)
	once := e.add(alice, "once", at(18, 0), nil)
	got, changed, err := e.svc.Complete(ctx, alice, once.ID)
	e.must(err)
	if !changed || got.Status != model.TodoDone {
		t.Fatalf("first: changed=%v %+v", changed, got)
	}
	got, changed, err = e.svc.Complete(ctx, alice, once.ID)
	e.must(err)
	if changed || got.Status != model.TodoDone {
		t.Fatalf("second: changed=%v %+v", changed, got)
	}

	// Due in 10h: it advances. Then it is due in 34h: no change.
	rep := e.add(alice, "rep", at(22, 0), mustRule(t, "daily 22:00"))
	got, changed, err = e.svc.Complete(ctx, alice, rep.ID)
	e.must(err)
	if !changed || got.Status != model.TodoOpen || got.DueAt.Equal(*rep.DueAt) {
		t.Fatalf("advance: changed=%v %+v", changed, got)
	}
	again, changed, err := e.svc.Complete(ctx, alice, rep.ID)
	e.must(err)
	if changed || !again.DueAt.Equal(*got.DueAt) {
		t.Fatalf("far: changed=%v %+v", changed, again)
	}
}

// Two Complete calls at the same time on one open item, on two nodes,
// give one change: a one-off item is done once, and a repeating item
// advances by one occurrence.
func TestCompleteConcurrentChangesOnce(t *testing.T) {
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
			td := e.add(alice, c.name, at(13, 0), rule)
			svcs := []*Svc{e.svc, other, e.svc, other}
			var changes atomic.Int32
			var wg sync.WaitGroup
			start := make(chan struct{})
			for _, s := range svcs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, changed, err := s.Complete(ctx, alice, td.ID)
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
		})
	}
}

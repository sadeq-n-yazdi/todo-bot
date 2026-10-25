package storetest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
)

// ---- conditional todo write (service.Complete, #147) ----

// recurring stores td as a daily item and returns it.
func recurring(t *testing.T, d store.Store, td model.Todo) model.Todo {
	t.Helper()
	td.Recur, td.UpdatedAt = "daily", base
	if err := d.UpdateTodo(context.Background(), td); err != nil {
		t.Fatal(err)
	}
	return td
}

func testUpdateOpenTodo(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	d := open()
	chat(t, d, 1)
	at := base.Add(time.Hour)

	// A one-off item: only the status is checked, not the due date.
	once := todo(t, d, 1, "once", tp(1))
	w := once
	w.Status, w.DoneAt, w.UpdatedAt = model.TodoDone, &at, at
	if ok, err := d.UpdateOpenTodo(ctx, w, tp(5)); err != nil || !ok {
		t.Fatalf("open once: ok=%v err=%v", ok, err)
	}
	if g, _ := d.GetTodo(ctx, once.ID); g.Status != model.TodoDone || g.DoneAt == nil || !g.DoneAt.Equal(at) {
		t.Fatalf("once = %+v", g)
	}
	// It is done now: a second write changes nothing.
	w.Text = "again"
	if ok, err := d.UpdateOpenTodo(ctx, w, tp(1)); err != nil || ok {
		t.Fatalf("done once: ok=%v err=%v", ok, err)
	}
	if g, _ := d.GetTodo(ctx, once.ID); g.Text != "once" {
		t.Fatalf("done once was written: %+v", g)
	}

	// A repeating item: the stored due date must still be due.
	rep := recurring(t, d, todo(t, d, 1, "rep", tp(1)))
	w = rep
	w.DueAt, w.UpdatedAt = tp(25), at
	if ok, err := d.UpdateOpenTodo(ctx, w, tp(2)); err != nil || ok {
		t.Fatalf("stale due: ok=%v err=%v", ok, err)
	}
	if g, _ := d.GetTodo(ctx, rep.ID); g.DueAt == nil || !g.DueAt.Equal(*tp(1)) {
		t.Fatalf("stale due was written: %+v", g)
	}
	if ok, err := d.UpdateOpenTodo(ctx, w, nil); err != nil || ok {
		t.Fatalf("nil due on a dated row: ok=%v err=%v", ok, err)
	}
	if ok, err := d.UpdateOpenTodo(ctx, w, tp(1)); err != nil || !ok {
		t.Fatalf("same due: ok=%v err=%v", ok, err)
	}
	if g, _ := d.GetTodo(ctx, rep.ID); g.DueAt == nil || !g.DueAt.Equal(*tp(25)) || g.Status != model.TodoOpen {
		t.Fatalf("rep = %+v", g)
	}

	// A repeating item with no due date matches only nil.
	undated := recurring(t, d, todo(t, d, 1, "undated", nil))
	w = undated
	w.DueAt = tp(3)
	if ok, err := d.UpdateOpenTodo(ctx, w, tp(3)); err != nil || ok {
		t.Fatalf("undated, due given: ok=%v err=%v", ok, err)
	}
	if ok, err := d.UpdateOpenTodo(ctx, w, nil); err != nil || !ok {
		t.Fatalf("undated, nil: ok=%v err=%v", ok, err)
	}

	if ok, err := d.UpdateOpenTodo(ctx, model.Todo{ID: 99999, Status: model.TodoDone}, nil); err != nil || ok {
		t.Fatalf("missing: ok=%v err=%v", ok, err)
	}
}

// Of several handles that complete one item with the same due date at the
// same time, exactly one writes it: for a one-off item and a repeating one.
func testUpdateOpenTodoConcurrent(t *testing.T, open func() store.Store) {
	ctx := context.Background()
	hs := handles(open, 4)
	chat(t, hs[0], 1)
	once := todo(t, hs[0], 1, "once", tp(1))
	rep := recurring(t, hs[0], todo(t, hs[0], 1, "rep", tp(1)))
	for _, c := range []struct {
		name string
		td   model.Todo
		edit func(w *model.Todo, i int)
	}{
		{"once", once, func(w *model.Todo, i int) {
			at := base.Add(time.Duration(i) * time.Minute)
			w.Status, w.DoneAt, w.UpdatedAt = model.TodoDone, &at, at
		}},
		{"repeating", rep, func(w *model.Todo, i int) {
			w.DueAt, w.UpdatedAt = tp(25+i), base.Add(time.Minute)
		}},
	} {
		var mu sync.Mutex
		wins := 0
		parallel(8, func(i int) {
			w := c.td
			c.edit(&w, i)
			ok, err := hs[i%len(hs)].UpdateOpenTodo(ctx, w, c.td.DueAt)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		})
		if wins != 1 {
			t.Fatalf("%s: %d winners, want 1", c.name, wins)
		}
	}
}

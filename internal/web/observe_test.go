package web

import (
	"context"
	"errors"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

func TestObservePassesThrough(t *testing.T) {
	h := newHarness(t)
	want := errors.New("next failed")
	var seen tg.Update
	next := func(_ context.Context, u tg.Update) error { seen = u; return want }
	wrapped := Observe(next, h.svc, map[int64]bool{idAnn: true}, nil)
	if err := wrapped(context.Background(), tg.Update{ID: 7}); !errors.Is(err, want) || seen.ID != 7 {
		t.Fatalf("err %v, update %+v", err, seen)
	}
}

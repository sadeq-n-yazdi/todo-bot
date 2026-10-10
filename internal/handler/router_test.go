package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
)

// prefsOnly implements only Prefs; any other call panics (nil embedded interface).
type prefsOnly struct{ service.Service }

func (prefsOnly) Prefs(context.Context, int64, string) (model.Prefs, error) {
	return model.Prefs{TZ: time.UTC, Locale: i18n.Default("en")}, nil
}

func newTestHandler() (*Handler, *tgtest.FakeSender) {
	fs := &tgtest.FakeSender{}
	h := New(Deps{Svc: prefsOnly{}, Send: fs, Allowed: map[int64]bool{1: true}, BotUsername: "TodoBot"})
	return h, fs
}

func TestWhoamiAnswersAnyoneInPrivate(t *testing.T) {
	h, fs := newTestHandler()
	h.Handle(context.Background(), tg.Update{Message: &tg.Message{ChatID: 99, ChatKind: model.ChatPrivate, FromID: 99, Text: "/whoami"}})
	last, ok := fs.Last()
	if !ok || !strings.Contains(last.Msg.Text, "99") {
		t.Fatalf("got %+v", fs.Messages())
	}
}

func TestWhoamiIgnoredInGroupForStrangers(t *testing.T) {
	h, fs := newTestHandler()
	h.Handle(context.Background(), tg.Update{Message: &tg.Message{ChatID: -5, ChatKind: model.ChatGroup, FromID: 99, Text: "/whoami"}})
	if len(fs.Messages()) != 0 {
		t.Fatalf("expected silence, got %+v", fs.Messages())
	}
}

func TestStrangersIgnored(t *testing.T) {
	h, fs := newTestHandler()
	h.Handle(context.Background(), tg.Update{Message: &tg.Message{ChatID: 99, ChatKind: model.ChatPrivate, FromID: 99, Text: "/list"}})
	h.Handle(context.Background(), tg.Update{Callback: &tg.Callback{ID: "c", FromID: 99, ChatID: 99, Data: "x"}})
	if len(fs.Messages()) != 0 {
		t.Fatalf("expected no messages, got %+v", fs.Messages())
	}
	if len(fs.Answers) != 1 || fs.Answers[0].Text != "" {
		t.Fatalf("callback should be answered silently, got %+v", fs.Answers)
	}
}

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		in, cmd, args string
		ok            bool
	}{
		{"/add buy milk", "add", "buy milk", true},
		{"/edit\t1 x", "edit", "1 x", true},
		{"/add\nbuy milk", "add", "buy milk", true},
		{"/List@TodoBot 2", "list", "2", true},
		{"/list@todobot", "list", "", true},
		{"/list@OtherBot", "", "", false},
		{"hello", "", "", false},
		{"/", "", "", false},
	}
	for _, c := range cases {
		cmd, args, ok := splitCommand(c.in, "TodoBot")
		if cmd != c.cmd || args != c.args || ok != c.ok {
			t.Errorf("splitCommand(%q) = %q,%q,%v", c.in, cmd, args, ok)
		}
	}
}

func TestPanicRecovered(t *testing.T) {
	fs := &tgtest.FakeSender{}
	// nil Service panics on Prefs; Handle must recover.
	h := New(Deps{Svc: prefsPanics{}, Send: fs, Allowed: map[int64]bool{1: true}})
	h.Handle(context.Background(), tg.Update{Message: &tg.Message{ChatID: 1, ChatKind: model.ChatPrivate, FromID: 1, Text: "/list"}})
}

type prefsPanics struct{ service.Service }

// NoteUsername is called by newReq on every update (#58); without these
// methods the call would panic on the nil embedded interface.
func (prefsOnly) NoteUsername(context.Context, int64, string) error { return nil }

func (prefsOnly) UserByUsername(context.Context, string) (model.User, error) {
	return model.User{}, service.ErrNotFound
}

func (prefsPanics) NoteUsername(context.Context, int64, string) error { return nil }

// dedupeSvc records MarkUpdate calls; seen ids return first=false.
type dedupeSvc struct {
	prefsOnly
	seen map[int64]bool
	err  error
}

func (d *dedupeSvc) MarkUpdate(_ context.Context, id int64) (bool, error) {
	if d.err != nil {
		return false, d.err
	}
	if d.seen[id] {
		return false, nil
	}
	d.seen[id] = true
	return true, nil
}

func TestHandleDedupesUpdates(t *testing.T) {
	fs := &tgtest.FakeSender{}
	svc := &dedupeSvc{seen: map[int64]bool{}}
	h := New(Deps{Svc: svc, Send: fs, Allowed: map[int64]bool{1: true}})
	u := tg.Update{ID: 42, Message: &tg.Message{ChatID: 99, ChatKind: model.ChatPrivate, FromID: 99, Text: "/whoami"}}
	for range 2 {
		if err := h.Handle(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(fs.Messages()); n != 1 {
		t.Fatalf("replayed update handled %d times, want 1", n)
	}

	svc.err = errors.New("db down")
	u.ID = 43
	if err := h.Handle(context.Background(), u); err == nil {
		t.Fatal("want error when the update cannot be recorded")
	}
	if n := len(fs.Messages()); n != 1 {
		t.Fatalf("unrecorded update was acted on (%d messages)", n)
	}
}

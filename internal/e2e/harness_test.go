// Package e2e holds end-to-end tests that run the real store, service,
// scheduler, handler and lease loop against a fake clock and fake Telegram.
package e2e

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/handler"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/scheduler"
	"github.com/sadeq-n-yazdi/todo-bot/internal/service"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	_ "github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite" // registers the "sqlite" scheme
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

const botName = "TodoBot"

// Users and chats used by the scenarios.
const (
	alice int64 = 1001
	bob   int64 = 1002
	carol int64 = 1003 // never allowlisted
	group int64 = -5001
)

func allowedUsers() map[int64]bool { return map[int64]bool{alice: true, bob: true} }

// dbURL returns the sqlite URL for todo.db in dir.
func dbURL(dir string) string {
	return (&url.URL{Scheme: "sqlite", Path: filepath.Join(dir, "todo.db")}).String()
}

// The stacks follow runBot in cmd/todo-bot/run.go, with these deliberate
// differences. Mirror any later change of run.go here.
//   - scheduler.WithMaxSleep is not passed (the scheduler default is used),
//     and service.Config.DefaultTZ is UTC instead of coming from config.
//   - The lead function does not call setCommands (it only deletes the
//     webhook and polls).
//   - Single-node tests deliver updates to Handle and drive the scheduler
//     with Tick, so they bypass the lease loop and the tgbot adapter and use
//     tgtest.FakeSender. Only the two-node tests (twonode_test.go) go
//     through lease.Run and tgbot against a mock Bot API.

// stack is one full bot node: store, service, scheduler and handler, wired
// like cmd/todo-bot/run.go does.
type stack struct {
	t     *testing.T
	st    store.Store
	svc   *service.Svc
	sched *scheduler.Scheduler
	h     *handler.Handler
	clk   *clocktest.Fake
	send  *tgtest.FakeSender
	node  string

	closed atomic.Bool
}

type stackOpts struct {
	node     string
	claimTTL time.Duration
	send     *tgtest.FakeSender // default: a new one
	sender   tg.Sender          // if set, used instead of send (send is then nil)
}

// newStack opens the database at db and builds a node on clk.
func newStack(t *testing.T, db string, clk *clocktest.Fake, o stackOpts) *stack {
	t.Helper()
	if o.node == "" {
		o.node = "node-a"
	}
	out := o.sender
	if out == nil {
		if o.send == nil {
			o.send = &tgtest.FakeSender{}
		}
		out = o.send
	}
	st, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.New(st, clk, service.Config{
		DefaultTZ: time.UTC, DefaultHour: 9, DefaultMinute: 0,
		NodeID: o.node, ClaimTTL: o.claimTTL,
	})
	svc.SetLogger(log)
	sched := scheduler.New(svc, out, clk, log, scheduler.WithClaimTTL(o.claimTTL))
	svc.SetWaker(sched)
	h := handler.New(handler.Deps{
		Svc: svc, Send: out, Parser: nlparse.Parser{DefaultHour: 9},
		Clock: clk, Allowed: allowedUsers(), BotUsername: botName, Log: log,
	})
	s := &stack{t: t, st: st, svc: svc, sched: sched, h: h, clk: clk, send: o.send, node: o.node}
	t.Cleanup(s.close)
	return s
}

// close closes the database; it is safe to call twice.
func (s *stack) close() {
	if s.closed.Swap(true) {
		return
	}
	if err := s.st.Close(); err != nil {
		s.t.Errorf("close store: %v", err)
	}
}

var updateSeq atomic.Int64

// say delivers a text message from a user in a chat. Every update gets a
// fresh, non-zero update id, so the MarkUpdate dedupe is part of the path.
func (s *stack) say(from, chat int64, text string) {
	s.t.Helper()
	s.sayAs(from, chat, text, "")
}

// sayAs is say with the sender's Telegram language code set.
func (s *stack) sayAs(from, chat int64, text, lang string) {
	s.t.Helper()
	kind := model.ChatPrivate
	title := ""
	if chat < 0 {
		kind, title = model.ChatGroup, "Team"
	}
	id := updateSeq.Add(1)
	s.deliver(tg.Update{ID: id, Message: &tg.Message{
		ID: int(id), ChatID: chat, ChatKind: kind, ChatTitle: title,
		FromID: from, FromName: "u", FromLang: lang, Text: text,
	}})
}

// press presses a button on message msgID in chat.
func (s *stack) press(from, chat int64, msgID int, data string) {
	s.t.Helper()
	s.deliver(tg.Update{ID: updateSeq.Add(1), Callback: &tg.Callback{
		ID: "cb-" + data, FromID: from, ChatID: chat, MessageID: msgID, Data: data,
	}})
}

func (s *stack) deliver(u tg.Update) {
	s.t.Helper()
	if err := s.h.Handle(context.Background(), u); err != nil {
		s.t.Fatalf("handle update %d: %v", u.ID, err)
	}
}

// tick runs the scheduler once at the current fake time.
func (s *stack) tick() { s.sched.Tick(context.Background()) }

// advance moves the fake clock by d and runs the scheduler once.
func (s *stack) advance(d time.Duration) {
	s.clk.Advance(d)
	s.tick()
}

// advanceTo sets the fake clock to t and runs the scheduler once.
func (s *stack) advanceTo(t time.Time) {
	s.clk.Set(t)
	s.tick()
}

// reminders returns the reminder messages (not edits, not replies) that f
// sent to chat.
func reminders(f *tgtest.FakeSender, chat int64) []tgtest.Sent {
	var out []tgtest.Sent
	for _, m := range f.Messages() {
		if !m.Edit && m.ChatID == chat && strings.HasPrefix(m.Msg.Text, "⏰ ") {
			out = append(out, m)
		}
	}
	return out
}

// buttonData finds the callback data of the button of m whose decoded action
// matches kind and arg.
func buttonData(t *testing.T, m tgtest.Sent, kind ui.ActionKind, arg string) string {
	t.Helper()
	for _, row := range m.Msg.Keyboard {
		for _, b := range row {
			a, err := ui.Decode(b.Data)
			if err == nil && a.Kind == kind && a.Arg == arg {
				return b.Data
			}
		}
	}
	t.Fatalf("no %s/%q button in %+v", kind, arg, m.Msg.Keyboard)
	return ""
}

// todoByNum loads the chat's todo number num.
func (s *stack) todoByNum(chat int64, num int) model.Todo {
	s.t.Helper()
	td, err := s.st.GetTodoByNum(context.Background(), chat, num)
	if err != nil {
		s.t.Fatalf("todo %d: %v", num, err)
	}
	return td
}

func utc(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return l
}

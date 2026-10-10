package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// runScheduler starts the scheduler's real Run loop and stops it, and waits
// for it, when the test ends or stop is called.
func (s *stack) runScheduler() (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.sched.Run(ctx); err != nil {
			s.t.Errorf("scheduler: %v", err)
		}
	}()
	stopped := false
	stop = func() {
		if !stopped {
			stopped = true
			cancel()
			<-done
		}
	}
	s.t.Cleanup(stop) // runs before the stack closes the database
	return stop
}

// Scenario 8: the process is stopped and started again on the same database
// file. Pending reminders, the user's settings, digests and the update
// dedupe all survive, and what came due meanwhile is caught up on at once.
func TestRestartKeepsPendingReminders(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	db := dbURL(t.TempDir())

	first := newStack(t, db, clk, stackOpts{node: "before"})
	first.say(alice, alice, "/lang fa")
	first.say(alice, alice, "/add چای in 5m") // due while the bot is down
	first.say(alice, alice, "/add tea in 3h") // due after the restart
	first.say(alice, alice, "/add stretch every day at 9:00")
	first.say(alice, alice, "/digest 08:00")
	const replayedID = 777
	first.deliver(tg.Update{ID: replayedID, Message: &tg.Message{
		ID: 1, ChatID: alice, ChatKind: model.ChatPrivate, FromID: alice, Text: "/add once only",
	}})
	if n := len(reminders(first.send, alice)); n != 0 {
		t.Fatalf("%d reminders before the restart", n)
	}
	first.close() // the process stops; nothing is pending in memory

	// The bot is down for 10 minutes.
	clk.Advance(10 * time.Minute)

	second := newStack(t, db, clk, stackOpts{node: "after"})
	stop := second.runScheduler() // its first tick is the catch-up
	waitUntil(t, "the catch-up reminder", func() bool { return len(reminders(second.send, alice)) == 1 })
	got := reminders(second.send, alice)
	if !strings.Contains(got[0].Msg.Text, "چای") || !strings.Contains(got[0].Msg.Text, "با تأخیر") {
		t.Errorf("catch-up reminder %q: want the Farsi text with the late marker", got[0].Msg.Text)
	}

	// The run loop now sleeps until the next due time; moving the fake clock
	// there wakes it. "tea" is due 3h after 12:00.
	waitUntil(t, "the scheduler to sleep", func() bool { return clk.Waiters() > 0 })
	clk.Set(utc(2026, 10, 10, 15, 0))
	waitUntil(t, "the second reminder", func() bool { return len(reminders(second.send, alice)) == 2 })
	if text := reminders(second.send, alice)[1].Msg.Text; !strings.Contains(text, "tea") || strings.Contains(text, "با تأخیر") {
		t.Errorf("second reminder %q", text)
	}

	// The recurring item and the digest were stored too.
	waitUntil(t, "the scheduler to sleep again", func() bool { return clk.Waiters() > 0 })
	clk.Set(utc(2026, 10, 11, 8, 0))
	waitUntil(t, "the digest", func() bool { return len(digestsFa(second)) == 1 })
	clk.Set(utc(2026, 10, 11, 9, 0))
	waitUntil(t, "the daily reminder", func() bool { return len(reminders(second.send, alice)) == 3 })
	if text := reminders(second.send, alice)[2].Msg.Text; !strings.Contains(text, "stretch") {
		t.Errorf("third reminder %q", text)
	}
	stop()

	// An update handled before the restart is still recognised as handled.
	second.send.Reset()
	second.deliver(tg.Update{ID: replayedID, Message: &tg.Message{
		ID: 1, ChatID: alice, ChatKind: model.ChatPrivate, FromID: alice, Text: "/add once only",
	}})
	if msgs := second.send.Messages(); len(msgs) != 0 {
		t.Errorf("a replayed update was answered after the restart: %+v", msgs)
	}
	n, err := second.st.CountTodos(context.Background(), model.TodoQuery{ChatID: alice})
	if err != nil || n != 4 {
		t.Errorf("alice has %d todos (err %v), want 4", n, err)
	}
}

// digestsFa returns the digests alice's node sent (alice's language is Farsi).
func digestsFa(s *stack) []string {
	var out []string
	for _, m := range s.send.Messages() {
		if !m.Edit && m.ChatID == alice && strings.Contains(m.Msg.Text, "📅") {
			out = append(out, m.Msg.Text)
		}
	}
	return out
}

// Scenario 7d: the MarkUpdate dedupe on its own, without a transport: the
// same update id, delivered twice, is acted on once.
func TestReplayedUpdateIDIsDropped(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})
	u := tg.Update{ID: 4242, Message: &tg.Message{
		ID: 1, ChatID: alice, ChatKind: model.ChatPrivate, FromID: alice, Text: "/add water the plants",
	}}
	s.deliver(u)
	s.deliver(u)
	if n := len(s.send.Messages()); n != 1 {
		t.Errorf("%d replies to one update delivered twice, want 1", n)
	}
	n, err := s.st.CountTodos(context.Background(), model.TodoQuery{ChatID: alice})
	if err != nil || n != 1 {
		t.Errorf("%d todos (err %v), want 1", n, err)
	}
	// A different id is a different update.
	u.ID++
	s.deliver(u)
	if n, _ := s.st.CountTodos(context.Background(), model.TodoQuery{ChatID: alice}); n != 2 {
		t.Errorf("%d todos after a new update, want 2", n)
	}
}

package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
)

// digests returns the digest messages (they have no keyboard and contain the
// section titles) sent to chat.
func digests(f *tgtest.FakeSender, chat int64) []tgtest.Sent {
	var out []tgtest.Sent
	for _, m := range f.Messages() {
		if m.Edit || m.ChatID != chat {
			continue
		}
		if strings.Contains(m.Msg.Text, "📅 Today") || strings.Contains(m.Msg.Text, "⚠️ Overdue") ||
			strings.Contains(m.Msg.Text, "without date") {
			out = append(out, m)
		}
	}
	return out
}

// Scenario 5a: /digest 08:00, then the digest lists the open items at 08:00.
func TestDailyDigest(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})

	s.say(alice, alice, "/add call the bank in 1h") // overdue by tomorrow
	s.say(alice, alice, "/add dentist tomorrow 15:00")
	s.say(alice, alice, "/add buy milk")
	s.say(alice, alice, "/add done already")
	s.say(alice, alice, "/done 4")
	s.say(alice, alice, "/digest 08:00")
	if last, _ := s.send.Last(); !strings.Contains(last.Msg.Text, "08:00") {
		t.Fatalf("/digest reply %q", last.Msg.Text)
	}
	assertDigestNext(t, s, alice, utc(2026, 10, 11, 8, 0))

	s.advanceTo(utc(2026, 10, 11, 7, 59))
	if n := len(digests(s.send, alice)); n != 0 {
		t.Fatalf("digest sent at 07:59 (%d)", n)
	}
	s.advanceTo(utc(2026, 10, 11, 8, 0))
	got := digests(s.send, alice)
	if len(got) != 1 {
		t.Fatalf("got %d digests at 08:00, want 1", len(got))
	}
	text := got[0].Msg.Text
	for _, want := range []string{"⚠️ Overdue", "call the bank", "📅 Today", "dentist", "+1 without date"} {
		if !strings.Contains(text, want) {
			t.Errorf("digest %q lacks %q", text, want)
		}
	}
	if strings.Contains(text, "done already") {
		t.Errorf("digest lists a completed item: %q", text)
	}
	assertDigestNext(t, s, alice, utc(2026, 10, 12, 8, 0))

	// It is sent once per day, not on every tick.
	s.advanceTo(utc(2026, 10, 11, 20, 0))
	if n := len(digests(s.send, alice)); n != 1 {
		t.Errorf("%d digests by the evening, want 1", n)
	}
	// /digest off stops it.
	s.say(alice, alice, "/digest off")
	s.advanceTo(utc(2026, 10, 12, 8, 0))
	if n := len(digests(s.send, alice)); n != 1 {
		t.Errorf("a digest was sent after /digest off (%d)", n)
	}
}

// Scenario 5b: a group's digest lists the group's shared list, whoever added
// the items, and goes to the group chat.
func TestGroupDigestUsesSharedList(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})

	s.say(alice, alice, "/add private secret") // alice's own list, not the group's
	s.say(alice, group, "/add book the venue tomorrow 10:00")
	s.say(bob, group, "/add order cake")
	s.say(alice, group, "/digest 08:00")

	s.advanceTo(utc(2026, 10, 11, 8, 0))
	got := digests(s.send, group)
	if len(got) != 1 {
		t.Fatalf("got %d group digests, want 1", len(got))
	}
	text := got[0].Msg.Text
	for _, want := range []string{"book the venue", "+1 without date"} {
		if !strings.Contains(text, want) {
			t.Errorf("group digest %q lacks %q", text, want)
		}
	}
	if strings.Contains(text, "secret") {
		t.Errorf("group digest leaks a private item: %q", text)
	}
	if n := len(digests(s.send, alice)); n != 0 {
		t.Errorf("the group digest was also sent to alice's private chat (%d)", n)
	}
}

// Scenario 6: two allowlisted users share one list in a group, commands
// addressed with @botname are routed, and a user who is not on the allowlist
// gets nothing except a private /whoami.
func TestGroupSharedListAndAllowlist(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})
	ctx := context.Background()

	s.say(alice, group, "/add@"+botName+" milk")
	s.say(bob, group, "/add@"+strings.ToLower(botName)+" bread") // case-insensitive
	s.say(alice, group, "/add@SomeOtherBot eggs")                // not for us
	s.say(alice, group, "/add@"+botName)                         // empty text: a usage reply, no item

	todos, err := s.st.ListTodos(ctx, model.TodoQuery{ChatID: group, Statuses: []model.TodoStatus{model.TodoOpen}})
	if err != nil {
		t.Fatal(err)
	}
	if len(todos) != 2 || todos[0].Text != "milk" || todos[1].Text != "bread" {
		t.Fatalf("group list = %+v, want milk and bread", todos)
	}
	if todos[0].Num != 1 || todos[1].Num != 2 || todos[0].CreatedBy != alice || todos[1].CreatedBy != bob {
		t.Errorf("numbers/creators wrong: %+v", todos)
	}

	// Either user sees both items, and either can complete the other's item.
	s.say(bob, group, "/list@"+botName)
	list, _ := s.send.Last()
	if list.ChatID != group || !strings.Contains(list.Msg.Text, "milk") || !strings.Contains(list.Msg.Text, "bread") {
		t.Errorf("/list in the group: %+v", list)
	}
	s.say(bob, group, "/done@"+botName+" 1")
	if td := s.todoByNum(group, 1); td.Status != model.TodoDone {
		t.Errorf("bob could not complete alice's item: %q", td.Status)
	}
	// Private lists are separate from the group's.
	s.say(alice, alice, "/list")
	if last, _ := s.send.Last(); strings.Contains(last.Msg.Text, "bread") {
		t.Errorf("the group's item leaked into alice's private list: %q", last.Msg.Text)
	}

	// carol is not allowlisted.
	s.send.Reset()
	s.say(carol, group, "/add hijack")
	s.say(carol, group, "/list")
	s.say(carol, group, "/whoami")
	s.say(carol, group, "/whoami@"+botName)
	s.say(carol, carol, "/add hijack")
	s.say(carol, carol, "/list")
	s.say(carol, carol, "just some text")
	s.press(carol, group, 99, "td:2")
	if msgs := s.send.Messages(); len(msgs) != 0 {
		t.Fatalf("a non-allowlisted user got replies: %+v", msgs)
	}
	if len(s.send.Answers) != 1 || s.send.Answers[0].Text != "" {
		t.Errorf("callback answers = %+v, want one empty answer", s.send.Answers)
	}
	if td := s.todoByNum(group, 2); td.Status != model.TodoOpen {
		t.Errorf("carol's button press changed item 2: %q", td.Status)
	}
	for _, chat := range []int64{group, carol} {
		n, err := s.st.CountTodos(ctx, model.TodoQuery{ChatID: chat})
		if err != nil {
			t.Fatal(err)
		}
		if want := map[int64]int{group: 2, carol: 0}[chat]; n != want {
			t.Errorf("chat %d has %d todos, want %d", chat, n, want)
		}
	}

	// /whoami in a private chat is the one thing carol is answered.
	s.say(carol, carol, "/whoami")
	msgs := s.send.Messages()
	if len(msgs) != 1 || msgs[0].ChatID != carol || msgs[0].Msg.Text != "<code>1003</code>" {
		t.Errorf("private /whoami reply = %+v", msgs)
	}
}

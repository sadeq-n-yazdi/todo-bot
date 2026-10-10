package e2e

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/handler"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Assignment scenarios (#58). "dad_home" (alice, English) adds a task in the
// family group for "sara_home" (bob, Farsi); the reminder goes to bob's
// private chat.
const (
	dadName  = "dad_home"
	saraName = "sara_home"
	// sara2 is a third, allowlisted account that later takes the name
	// sara_home.
	sara2      int64 = 1003
	assignTask       = "pick up our son from school"
)

// sayU delivers a text message from a user with a Telegram @username, through
// handler h (s.h unless a test needs another allowlist).
func (s *stack) sayU(h *handler.Handler, from int64, username string, chat int64, text string) {
	s.t.Helper()
	kind, title := model.ChatPrivate, ""
	if chat < 0 {
		kind, title = model.ChatGroup, "Family"
	}
	id := updateSeq.Add(1)
	u := tg.Update{ID: id, Message: &tg.Message{
		ID: int(id), ChatID: chat, ChatKind: kind, ChatTitle: title,
		FromID: from, FromName: "u", FromUsername: username, Text: text,
	}}
	if err := h.Handle(context.Background(), u); err != nil {
		s.t.Fatalf("handle update %d: %v", id, err)
	}
}

func (s *stack) sayDad(chat int64, text string) {
	s.t.Helper()
	s.sayU(s.h, alice, dadName, chat, text)
}

func (s *stack) saySara(chat int64, text string) {
	s.t.Helper()
	s.sayU(s.h, bob, saraName, chat, text)
}

// pressSara presses a button as sara on message msgID in chat.
func (s *stack) pressSara(chat int64, msgID int, data string) {
	s.t.Helper()
	err := s.h.Handle(context.Background(), tg.Update{ID: updateSeq.Add(1), Callback: &tg.Callback{
		ID: "cb-" + data, FromID: bob, FromUsername: saraName, ChatID: chat, MessageID: msgID, Data: data,
	}})
	if err != nil {
		s.t.Fatalf("press: %v", err)
	}
}

// texts returns the texts of all new (non-edit) messages sent to chat.
func texts(s *stack, chat int64) []string {
	var out []string
	for _, m := range s.send.Messages() {
		if !m.Edit && m.ChatID == chat {
			out = append(out, m.Msg.Text)
		}
	}
	return out
}

func countContaining(ss []string, sub string) int {
	n := 0
	for _, x := range ss {
		if strings.Contains(x, sub) {
			n++
		}
	}
	return n
}

// assignSetup builds a stack at 10:00 UTC where sara (Farsi) has started the
// bot and dad has added the task in the family group.
func assignSetup(t *testing.T) *stack {
	t.Helper()
	clk := clocktest.New(utc(2026, 10, 10, 10, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})
	s.saySara(bob, "/lang fa") // sara's first message: her username is learned
	s.sayDad(group, "/add@"+botName+" @"+saraName+" "+assignTask+" today 15:00")
	return s
}

// Scenario 1: the confirmation names the assignee; at 15:00 the reminder goes
// to the assignee's private chat in her language, with buttons, not to the
// group.
func TestAssignReminderGoesToAssigneeDM(t *testing.T) {
	s := assignSetup(t)

	g := texts(s, group)
	if len(g) != 1 || !strings.Contains(g[0], "@"+saraName) || !strings.Contains(g[0], assignTask) {
		t.Fatalf("group confirmation %q, want one message naming @%s", g, saraName)
	}
	td := s.todoByNum(group, 1)
	if td.AssigneeID != bob || td.CreatedBy != alice || td.Text != assignTask {
		t.Fatalf("todo %+v", td)
	}

	s.advanceTo(utc(2026, 10, 10, 14, 59))
	if n := len(reminders(s.send, bob)) + len(reminders(s.send, group)); n != 0 {
		t.Fatalf("%d reminders a minute early", n)
	}
	s.advanceTo(utc(2026, 10, 10, 15, 0))

	dm := reminders(s.send, bob)
	if len(dm) != 1 {
		t.Fatalf("%d reminders in the assignee's DM, want 1", len(dm))
	}
	if !strings.Contains(dm[0].Msg.Text, assignTask) {
		t.Errorf("reminder text %q", dm[0].Msg.Text)
	}
	buttonData(t, dm[0], ui.ActReminderDone, "")
	buttonData(t, dm[0], ui.ActSnooze, ui.Snooze1h)
	// The buttons are in Farsi, the assignee's language (the creator's is English).
	var labels []string
	for _, row := range dm[0].Msg.Keyboard {
		for _, b := range row {
			labels = append(labels, b.Text)
		}
	}
	if all := strings.Join(labels, " "); !strings.Contains(all, "انجام شد") || hasASCIIDigit(all) {
		t.Errorf("buttons %q are not Farsi", all)
	}
	if n := len(reminders(s.send, group)); n != 0 {
		t.Errorf("%d reminders in the group, want 0", n)
	}
	if n := len(reminders(s.send, alice)); n != 0 {
		t.Errorf("%d reminders in the creator's DM, want 0", n)
	}

	s.advance(time.Hour) // never re-sent
	if n := len(reminders(s.send, bob)); n != 1 {
		t.Errorf("%d DM reminders later, want 1", n)
	}
}

// Scenario 2: the assignee presses Done on the DM; the item is done and the
// group gets the "did" notice once, in the creator's (English) language.
func TestAssignDoneByAssigneeNotifiesGroup(t *testing.T) {
	s := assignSetup(t)
	s.advanceTo(utc(2026, 10, 10, 15, 0))
	dm := reminders(s.send, bob)
	if len(dm) != 1 {
		t.Fatalf("%d DM reminders, want 1", len(dm))
	}
	before := len(texts(s, group))
	s.pressSara(bob, dm[0].MsgID, buttonData(t, dm[0], ui.ActReminderDone, ""))

	if td := s.todoByNum(group, 1); td.Status != model.TodoDone {
		t.Fatalf("status %q, want done", td.Status)
	}
	g := texts(s, group)[before:]
	if len(g) != 1 || !strings.HasPrefix(g[0], "✅ ") || !strings.Contains(g[0], " did: ") ||
		!strings.Contains(g[0], saraName) || !strings.Contains(g[0], assignTask) {
		t.Fatalf("group got %q, want one English did notice for @%s", g, saraName)
	}
	s.advance(24 * time.Hour)
	if n := countContaining(texts(s, group), " did: "); n != 1 {
		t.Errorf("%d did notices, want 1", n)
	}
}

// Scenario 3: the creator sees the item in the group /list with the assignee
// and can complete it from the group; there is no did notice then.
func TestAssignCreatorSeesAndCompletesInGroup(t *testing.T) {
	s := assignSetup(t)
	s.sayDad(group, "/list@"+botName)
	l, _ := s.send.Last()
	if !strings.Contains(l.Msg.Text, assignTask) || !strings.Contains(l.Msg.Text, "→ <a href=\"tg://user?id=1002\">@"+saraName+"</a>") {
		t.Fatalf("group list %q lacks the item with → @%s", l.Msg.Text, saraName)
	}
	s.sayDad(group, "/done@"+botName+" 1")
	if td := s.todoByNum(group, 1); td.Status != model.TodoDone {
		t.Fatalf("status %q, want done", td.Status)
	}
	s.advance(8 * time.Hour)
	if n := countContaining(texts(s, group), " did: "); n != 0 {
		t.Errorf("%d did notices after the creator completed it, want 0", n)
	}
	if n := len(reminders(s.send, bob)); n != 0 {
		t.Errorf("%d reminders for a completed item", n)
	}
}

// Scenario 4: the assignee's private /list has the "Assigned to me" section,
// in her language.
func TestAssignAppearsInAssigneePrivateList(t *testing.T) {
	s := assignSetup(t)
	s.saySara(bob, "/list")
	l, _ := s.send.Last()
	if !strings.Contains(l.Msg.Text, assignTask) || !strings.Contains(l.Msg.Text, "سپرده\u200cشده به من") {
		t.Fatalf("Farsi private list %q lacks the item or the heading", l.Msg.Text)
	}

	s.saySara(bob, "/lang en")
	s.saySara(bob, "/list")
	l, _ = s.send.Last()
	if !strings.Contains(l.Msg.Text, "Assigned to me") || !strings.Contains(l.Msg.Text, assignTask) {
		t.Errorf("English private list %q lacks 'Assigned to me' or the item", l.Msg.Text)
	}
}

// Scenario 5: a well-formed @username the bot has never seen creates nothing.
func TestAssignUnknownUsernameCreatesNothing(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 10, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})
	s.sayDad(group, "/add @nobody_here buy bread today 15:00")
	g := texts(s, group)
	if len(g) != 1 || !strings.Contains(g[0], "@nobody_here hasn't started the bot") {
		t.Fatalf("reply %q, want the 'hasn't started the bot' message", g)
	}
	if n, err := s.st.CountTodos(context.Background(), model.TodoQuery{ChatID: group}); err != nil || n != 0 {
		t.Errorf("%d todos (err %v), want 0", n, err)
	}
	s.advance(8 * time.Hour)
	if len(reminders(s.send, group))+len(reminders(s.send, alice)) != 0 {
		t.Error("a reminder was sent for a refused item")
	}

	// The same refusal in Farsi, for a Farsi creator.
	s.saySara(bob, "/lang fa")
	s.send.Reset()
	s.saySara(group, "/add @nobody_here buy bread")
	g = texts(s, group)
	if len(g) != 1 || !strings.Contains(g[0], "هنوز ربات را شروع نکرده") {
		t.Errorf("Farsi reply %q", g)
	}
}

// Scenario 6: the Bot API answers 403 for the assignee's DM. The group gets
// exactly one message, prefixed with a tg://user?id mention, and it is not
// sent again on later ticks.
func TestAssignBlockedAssigneeFallsBackToGroupOnce(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 10, 0))
	m := newMockTelegram(t)
	a := startNode(t, m, dbURL(t.TempDir()), clk, "node-a")

	a.sayU(a.h, bob, saraName, bob, "/help")
	a.sayU(a.h, alice, dadName, group, "/add @"+saraName+" "+assignTask+" today 15:00")
	m.blockChat(bob)

	a.advanceTo(utc(2026, 10, 10, 15, 0))
	a.tick()
	a.advance(time.Hour)
	a.advance(24 * time.Hour)

	var toGroup, toSara int
	for _, c := range m.sent("⏰ ") {
		switch c.ChatID {
		case bob:
			toSara++
		case group:
			toGroup++
			if !strings.HasPrefix(c.Text, `<a href="tg://user?id=1002">`) || !strings.Contains(c.Text, assignTask) {
				t.Errorf("fallback %q is not the reminder prefixed with the mention", c.Text)
			}
		}
	}
	if toGroup != 1 {
		t.Errorf("%d fallback messages in the group, want 1", toGroup)
	}
	if toSara != 1 {
		t.Errorf("%d DM attempts, want 1 (a permanent error is not retried)", toSara)
	}
}

// Scenario 7: a username moves to another account. A new /add resolves to the
// new owner and the old account loses the name.
func TestAssignUsernameMovesToNewAccount(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 10, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})
	allowed := allowedUsers()
	allowed[sara2] = true
	h2 := handler.New(handler.Deps{
		Svc: s.svc, Send: s.send, Parser: nlparse.Parser{DefaultHour: 9},
		Clock: clk, Allowed: allowed, BotUsername: botName,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx := context.Background()

	s.saySara(bob, "/help")
	if u, err := s.st.UserByUsername(ctx, saraName); err != nil || u.ID != bob {
		t.Fatalf("before the move: %+v, %v", u, err)
	}
	s.sayU(h2, sara2, saraName, sara2, "/help")

	u, err := s.st.UserByUsername(ctx, saraName)
	if err != nil || u.ID != sara2 {
		t.Fatalf("after the move: %+v, %v; want user %d", u, err, sara2)
	}
	if old, err := s.st.GetUser(ctx, bob); err != nil || old.Username != "" {
		t.Errorf("old account %+v (err %v), want the name cleared", old, err)
	}

	s.sayU(h2, alice, dadName, group, "/add @"+saraName+" "+assignTask+" today 15:00")
	if td := s.todoByNum(group, 1); td.AssigneeID != sara2 {
		t.Errorf("assignee %d, want %d", td.AssigneeID, sara2)
	}
	s.advanceTo(utc(2026, 10, 10, 15, 0))
	if n := len(reminders(s.send, sara2)); n != 1 {
		t.Errorf("%d reminders to the new owner, want 1", n)
	}
	if n := len(reminders(s.send, bob)); n != 0 {
		t.Errorf("%d reminders to the old owner, want 0", n)
	}
}

// Scenario 8: two nodes share the database and tick together; the DM
// reminder is delivered exactly once.
func TestAssignTwoNodesDeliverDMOnce(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 10, 0))
	m := newMockTelegram(t)
	db := dbURL(t.TempDir())
	a := startNode(t, m, db, clk, "node-a")
	b := startNode(t, m, db, clk, "node-b")

	a.sayU(a.h, bob, saraName, bob, "/help")
	a.sayU(a.h, alice, dadName, group, "/add @"+saraName+" "+assignTask+" today 15:00")

	clk.Set(utc(2026, 10, 10, 15, 0))
	tickAll(a, b)
	tickAll(b, a)
	clk.Advance(10 * time.Minute)
	tickAll(a, b)

	var dm, grp int
	for _, c := range reminderSends(m) {
		switch c.ChatID {
		case bob:
			dm++
		case group:
			grp++
		}
	}
	if dm != 1 || grp != 0 {
		t.Errorf("DM reminders %d (want 1), group reminders %d (want 0)", dm, grp)
	}
}

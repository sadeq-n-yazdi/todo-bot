package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// openPrompt presses a prompt button and returns the ForceReply message ID.
func openPrompt(t *testing.T, hh *harness12, user, chat int64, k ui.ActionKind, todoID int64) int {
	t.Helper()
	hh.fs.Reset()
	hh.press(user, chat, 4, act(k, todoID, ""))
	s := hh.last()
	if s.Edit || !s.Msg.ForceReply {
		t.Fatalf("want a ForceReply prompt, got %+v", s)
	}
	p, ok := hh.svc.prompts[[2]int64{chat, int64(s.MsgID)}]
	if !ok {
		t.Fatalf("prompt not persisted: %+v", hh.svc.prompts)
	}
	wantKind := model.PromptEdit
	if k == ui.ActRemind {
		wantKind = model.PromptRemind
	}
	if p.Kind != wantKind || p.TodoID != todoID || p.UserID != user {
		t.Fatalf("bad prompt %+v", p)
	}
	return s.MsgID
}

func TestEditPromptFlow(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(1, "buy milk")
	id := openPrompt(t, hh, 1, 1, ui.ActEdit, td.ID)

	hh.fs.Reset()
	hh.say(1, 1, model.ChatPrivate, "buy oat milk", id)

	if got := hh.svc.todo(td.ID).Text; got != "buy oat milk" {
		t.Fatalf("text = %q", got)
	}
	if !strings.Contains(hh.last().Msg.Text, "buy oat milk") {
		t.Fatalf("confirmation expected: %+v", hh.last())
	}
	if len(hh.svc.prompts) != 0 {
		t.Fatal("prompt should be consumed")
	}
}

func TestPromptSurvivesHandlerRebuild(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(1, "buy milk")
	id := openPrompt(t, hh, 1, 1, ui.ActEdit, td.ID)

	hh.h = hh.newHandler() // simulated restart: no in-memory state carries over
	hh.say(1, 1, model.ChatPrivate, "buy bread", id)

	if got := hh.svc.todo(td.ID).Text; got != "buy bread" {
		t.Fatalf("prompt lost across restart, text = %q", got)
	}
}

func TestExpiredPromptIgnored(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(-9, "group item")
	id := openPrompt(t, hh, 1, -9, ui.ActEdit, td.ID)

	hh.clk.Advance(11 * time.Minute)
	hh.fs.Reset()

	m := &tg.Message{ID: 9, ChatID: -9, ChatKind: model.ChatGroup, FromID: 1, Text: "too late", ReplyToID: id}
	r, err := hh.h.newReq(context.Background(), 1, "", -9, model.ChatGroup, "")
	if err != nil {
		t.Fatal(err)
	}
	handled, err := hh.h.onReply(context.Background(), m, r)
	if handled || err != nil {
		t.Fatalf("expired prompt: handled=%v err=%v", handled, err)
	}
	hh.say(1, -9, model.ChatGroup, "too late", id)
	if hh.svc.todo(td.ID).Text != "group item" || len(hh.fs.Messages()) != 0 {
		t.Fatalf("expired prompt must change nothing: %+v", hh.fs.Messages())
	}
}

func TestReplyFromOtherUserIgnoredAndKeepsPrompt(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(-9, "group item")
	id := openPrompt(t, hh, 1, -9, ui.ActEdit, td.ID)

	hh.fs.Reset()
	hh.say(2, -9, model.ChatGroup, "hijack", id)
	if hh.svc.todo(td.ID).Text != "group item" || len(hh.fs.Messages()) != 0 {
		t.Fatalf("other user's reply must be ignored: %+v", hh.fs.Messages())
	}
	if len(hh.svc.prompts) != 1 {
		t.Fatal("prompt must stay for its owner")
	}

	hh.say(1, -9, model.ChatGroup, "mine", id)
	if hh.svc.todo(td.ID).Text != "mine" {
		t.Fatal("owner can still answer")
	}
}

func TestEditPromptEmptyReplyKeepsPromptOpen(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(1, "keep")
	id := openPrompt(t, hh, 1, 1, ui.ActEdit, td.ID)

	hh.fs.Reset()
	hh.say(1, 1, model.ChatPrivate, "   ", id)
	if hh.svc.todo(td.ID).Text != "keep" || hh.last().Msg.Text == "" {
		t.Fatalf("want an explanation and no change: %+v", hh.fs.Messages())
	}
	if !hh.last().Msg.ForceReply {
		t.Fatalf("retry must be a fresh ForceReply prompt: %+v", hh.last())
	}
	id = hh.last().MsgID
	hh.say(1, 1, model.ChatPrivate, "better", id)
	if hh.svc.todo(td.ID).Text != "better" {
		t.Fatal("prompt should still be answerable after an empty reply")
	}
}

func TestRemindPromptFlow(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(1, "call mom")
	id := openPrompt(t, hh, 1, 1, ui.ActRemind, td.ID)

	hh.fs.Reset()
	hh.say(1, 1, model.ChatPrivate, "tomorrow 18:00", id)

	want := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	if len(hh.svc.remindAt) != 1 || !hh.svc.remindAt[0].Equal(want) {
		t.Fatalf("remindAt = %v, want %v", hh.svc.remindAt, want)
	}
	if !strings.Contains(hh.last().Msg.Text, "18:00") {
		t.Fatalf("confirmation should show the time: %q", hh.last().Msg.Text)
	}
}

func TestRemindPromptUsesPersianDigitsAndRetries(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(1, "call mom")
	id := openPrompt(t, hh, 1, 1, ui.ActRemind, td.ID)

	hh.fs.Reset()
	hh.say(1, 1, model.ChatPrivate, "blah blah", id)
	if len(hh.svc.remindAt) != 0 || hh.last().Msg.Text == "" {
		t.Fatalf("unparsable time should be explained: %+v", hh.fs.Messages())
	}
	if !hh.last().Msg.ForceReply {
		t.Fatalf("retry must be a fresh ForceReply prompt: %+v", hh.last())
	}
	// The fresh prompt can be answered; Persian digits are accepted.
	id = hh.last().MsgID
	hh.say(1, 1, model.ChatPrivate, "فردا ساعت ۱۸", id)
	want := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	if len(hh.svc.remindAt) != 1 || !hh.svc.remindAt[0].Equal(want) {
		t.Fatalf("remindAt = %v, want %v", hh.svc.remindAt, want)
	}
}

func TestMigrateCallsService(t *testing.T) {
	hh := newHarness12(t)
	err := hh.h.Handle(context.Background(), tg.Update{Message: &tg.Message{ChatID: -100, ChatKind: model.ChatGroup, MigrateToChatID: -1001}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hh.svc.migrated) != 1 || hh.svc.migrated[0] != [2]int64{-100, -1001} {
		t.Fatalf("migrated = %v", hh.svc.migrated)
	}
}

func TestDuplicateUpdateProcessedOnce(t *testing.T) {
	hh := newHarness12(t)
	a := hh.svc.addTodo(1, "x")
	u := tg.Update{ID: 7, Callback: &tg.Callback{ID: "c", FromID: 1, ChatID: 1, MessageID: 1, Data: act(ui.ActDone, a.ID, "")}}
	for range 2 {
		if err := hh.h.Handle(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	if hh.svc.called("Complete") != 1 {
		t.Fatalf("Complete called %d times", hh.svc.called("Complete"))
	}
}

func TestTakePromptErrorDoesNotFallThroughToAdd(t *testing.T) {
	hh := newHarness12(t)
	hh.svc.takeErr = errors.New("db down")
	hh.say(1, 1, model.ChatPrivate, "sneaky", 5)
	if hh.svc.called("AddTodo") != 0 {
		t.Fatal("a store error must not turn a reply into a new item")
	}
	if hh.last().Msg.Text == "" {
		t.Fatal("user should see an error")
	}
}

func TestLongItemTextIsTruncatedInEchoes(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(1, strings.Repeat("<", 3000))
	for _, k := range []ui.ActionKind{ui.ActDelete, ui.ActEdit, ui.ActRemind} {
		hh.fs.Reset()
		hh.press(1, 1, 4, act(k, td.ID, ""))
		if n := len([]rune(hh.last().Msg.Text)); n > 4096 {
			t.Errorf("%s: text has %d runes", k, n)
		}
	}
	hh.fs.Reset()
	hh.press(1, 1, 4, act(ui.ActDone, td.ID, ""))
	if n := len([]rune(hh.last().Msg.Text)); n > 4096 || !strings.Contains(hh.last().Msg.Text, "…") {
		t.Errorf("done: text has %d runes", n)
	}
}

func TestEditPromptServiceEmptyTextRetries(t *testing.T) {
	hh := newHarness12(t)
	hh.svc.emptyText = "blank"
	td := hh.svc.addTodo(1, "keep")
	id := openPrompt(t, hh, 1, 1, ui.ActEdit, td.ID)
	hh.fs.Reset()
	hh.say(1, 1, model.ChatPrivate, "blank", id)
	if hh.svc.todo(td.ID).Text != "keep" || !hh.last().Msg.ForceReply {
		t.Fatalf("want an unchanged item and a fresh prompt: %+v", hh.last())
	}
}

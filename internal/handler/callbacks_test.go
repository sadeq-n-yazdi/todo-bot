package handler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

func act(k ui.ActionKind, id int64, arg string) string {
	return ui.Encode(ui.Action{Kind: k, ID: id, Arg: arg})
}

func TestCallbackDoneEditsListInPlace(t *testing.T) {
	hh := newHarness12(t)
	a := hh.svc.addTodo(1, "buy milk")
	hh.svc.addTodo(1, "call mom")

	hh.press(1, 1, 77, act(ui.ActDone, a.ID, ""))

	if hh.svc.todo(a.ID).Status != model.TodoDone {
		t.Fatal("todo not completed")
	}
	got := hh.last()
	if !got.Edit || got.MsgID != 77 || got.ChatID != 1 {
		t.Fatalf("want in-place edit of message 77, got %+v", got)
	}
	if !strings.Contains(got.Msg.Text, "buy milk") || !strings.Contains(got.Msg.Text, "call mom") {
		t.Fatalf("edited text should mention the done item and show the rest: %q", got.Msg.Text)
	}
	if len(hh.fs.Answers) != 1 {
		t.Fatalf("want exactly one answer, got %+v", hh.fs.Answers)
	}
}

func TestCallbackForeignChatRejected(t *testing.T) {
	kinds := []ui.ActionKind{ui.ActDone, ui.ActDelete, ui.ActDeleteConfirm, ui.ActEdit, ui.ActRemind}
	for _, k := range kinds {
		t.Run(string(k), func(t *testing.T) {
			hh := newHarness12(t)
			foreign := hh.svc.addTodo(2, "secret")

			hh.press(1, 1, 5, act(k, foreign.ID, ""))

			if got := hh.svc.todo(foreign.ID); got.Status != model.TodoOpen || got.Text != "secret" {
				t.Fatalf("foreign todo was touched: %+v", got)
			}
			if n := len(hh.fs.Messages()); n != 0 {
				t.Fatalf("nothing should be sent or edited, got %+v", hh.fs.Messages())
			}
			if len(hh.svc.prompts) != 0 {
				t.Fatal("no prompt may be stored")
			}
			if len(hh.fs.Answers) != 1 || hh.fs.Answers[0].Text == "" {
				t.Fatalf("want one explanatory answer, got %+v", hh.fs.Answers)
			}
		})
	}
}

func TestCallbackForeignReminderRejected(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(2, "secret")
	rem := hh.svc.addReminder(2, td.ID)

	hh.press(1, 1, 5, act(ui.ActReminderDone, rem.ID, ""))
	hh.press(1, 1, 5, act(ui.ActSnooze, rem.ID, ui.Snooze1h))

	if hh.svc.todo(td.ID).Status != model.TodoOpen {
		t.Fatal("foreign reminder acknowledged")
	}
	if len(hh.fs.Messages()) != 0 {
		t.Fatalf("no edits expected, got %+v", hh.fs.Messages())
	}
	if len(hh.fs.Answers) != 2 || hh.fs.Answers[0].Text == "" || hh.fs.Answers[1].Text == "" {
		t.Fatalf("want two explanatory answers, got %+v", hh.fs.Answers)
	}
}

func TestCallbackDeleteNeedsConfirmation(t *testing.T) {
	hh := newHarness12(t)
	a := hh.svc.addTodo(1, "old thing")

	hh.press(1, 1, 9, act(ui.ActDelete, a.ID, ""))
	if hh.svc.todo(a.ID).Status != model.TodoOpen || hh.svc.called("Delete") != 0 {
		t.Fatal("delete must wait for confirmation")
	}
	conf := hh.last()
	if !conf.Edit || !strings.Contains(conf.Msg.Text, "old thing") {
		t.Fatalf("want an in-place confirmation, got %+v", conf)
	}
	var datas []string
	for _, row := range conf.Msg.Keyboard {
		for _, b := range row {
			datas = append(datas, b.Data)
		}
	}
	if len(datas) != 2 || datas[0] != act(ui.ActDeleteConfirm, a.ID, "") || datas[1] != act(ui.ActCancel, 0, "") {
		t.Fatalf("unexpected confirmation buttons %v", datas)
	}

	// Cancel restores the list and keeps the todo.
	hh.press(1, 1, 9, datas[1])
	if !strings.Contains(hh.last().Msg.Text, "old thing") || hh.svc.todo(a.ID).Status != model.TodoOpen {
		t.Fatalf("cancel should show the list again: %+v", hh.last())
	}

	hh.press(1, 1, 9, datas[0])
	if hh.svc.todo(a.ID).Status != model.TodoDeleted {
		t.Fatal("confirm should delete")
	}
	if len(hh.fs.Answers) != 3 {
		t.Fatalf("every press is answered once, got %d", len(hh.fs.Answers))
	}
}

func TestCallbackPaging(t *testing.T) {
	hh := newHarness12(t)
	for i := 1; i <= 12; i++ {
		hh.svc.addTodo(1, fmt.Sprintf("item%02d", i))
	}
	hh.press(1, 1, 3, act(ui.ActPage, 0, "1"))
	got := hh.last()
	if !got.Edit || got.MsgID != 3 {
		t.Fatalf("want in-place edit, got %+v", got)
	}
	if !strings.Contains(got.Msg.Text, "item11") || strings.Contains(got.Msg.Text, "item01") {
		t.Fatalf("page 2 expected: %q", got.Msg.Text)
	}
	// A page past the end is clamped to the last page.
	hh.press(1, 1, 3, act(ui.ActPage, 0, "9"))
	if !strings.Contains(hh.last().Msg.Text, "item12") {
		t.Fatalf("clamped page expected: %q", hh.last().Msg.Text)
	}
}

func TestCallbackReminderDoneAndSnooze(t *testing.T) {
	hh := newHarness12(t)
	td := hh.svc.addTodo(1, "pills")
	rem := hh.svc.addReminder(1, td.ID)

	hh.press(1, 1, 20, act(ui.ActSnooze, rem.ID, ui.Snooze1h))
	got := hh.last()
	if !got.Edit || got.MsgID != 20 || len(got.Msg.Keyboard) != 0 {
		t.Fatalf("snooze should edit in place and drop the buttons: %+v", got)
	}
	if !strings.Contains(got.Msg.Text, "13:00") {
		t.Fatalf("snooze text should show the new time: %q", got.Msg.Text)
	}

	hh.press(1, 1, 20, act(ui.ActReminderDone, rem.ID, ""))
	got = hh.last()
	if !got.Edit || !strings.Contains(got.Msg.Text, "pills") || len(got.Msg.Keyboard) != 0 {
		t.Fatalf("done should edit in place: %+v", got)
	}
	if hh.svc.todo(td.ID).Status != model.TodoDone {
		t.Fatal("todo should be done")
	}
}

func TestCallbackBadDataAnsweredSilently(t *testing.T) {
	hh := newHarness12(t)
	hh.press(1, 1, 1, "garbage")
	if len(hh.fs.Answers) != 1 || hh.fs.Answers[0].Text != "" || len(hh.fs.Messages()) != 0 {
		t.Fatalf("got %+v / %+v", hh.fs.Answers, hh.fs.Messages())
	}
}

func TestCallbackStaleTodoAnswered(t *testing.T) {
	hh := newHarness12(t)
	hh.press(1, 1, 1, act(ui.ActDone, 999, ""))
	if len(hh.fs.Answers) != 1 || hh.fs.Answers[0].Text == "" || len(hh.fs.Messages()) != 0 {
		t.Fatalf("got %+v / %+v", hh.fs.Answers, hh.fs.Messages())
	}
}

func TestCallbackDoneItemAnsweredByServiceVerdict(t *testing.T) {
	hh := newHarness12(t)
	a := hh.svc.addTodo(1, "finished elsewhere")
	hh.press(1, 1, 1, act(ui.ActDone, a.ID, ""))
	hh.fs.Reset()
	// A stale button: the item is already done. The service says not-found
	// for deletion of a deleted item; here Complete succeeds again in the
	// fake, so use delete-confirm twice instead.
	hh.press(1, 1, 1, act(ui.ActDeleteConfirm, a.ID, ""))
	hh.press(1, 1, 1, act(ui.ActDeleteConfirm, a.ID, ""))
	if hh.svc.called("Delete") != 2 {
		t.Fatalf("the service must be asked, calls=%d", hh.svc.called("Delete"))
	}
	if last := hh.fs.Answers[len(hh.fs.Answers)-1]; last.Text == "" {
		t.Fatalf("stale press should be answered with an explanation: %+v", hh.fs.Answers)
	}
}

func TestCallbackEditPromptOnDoneItemAllowed(t *testing.T) {
	hh := newHarness12(t)
	a := hh.svc.addTodo(1, "x")
	hh.press(1, 1, 1, act(ui.ActDone, a.ID, ""))
	hh.fs.Reset()
	hh.press(1, 1, 1, act(ui.ActEdit, a.ID, ""))
	if !hh.last().Msg.ForceReply {
		t.Fatalf("Todo includes done items, so the prompt is sent: %+v", hh.last())
	}
}

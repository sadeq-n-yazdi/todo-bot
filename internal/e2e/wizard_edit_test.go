package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// The edit wizard (#98, #106): the ✏️ button of /list opens the wizard, a
// new time moves the reminder, and the old time no longer fires.
func TestEditWizardMovesTheReminder(t *testing.T) {
	s := newWizardStack(t) // Saturday 2026-10-10 12:00 UTC
	s.wsay(alice, alice, "/add buy milk tomorrow 10:00", 0)
	if td := s.todoByNum(alice, 1); td.DueAt == nil || !td.DueAt.Equal(utc(2026, 10, 11, 10, 0)) {
		t.Fatalf("item %+v", td)
	}

	// The ✏️ button of the list opens the edit card.
	s.wsay(alice, alice, "/list", 0)
	list, ok := s.newest(alice, func(m tgtest.Sent) bool { return strings.Contains(m.Msg.Text, "buy milk") })
	if !ok {
		t.Fatal("no list message")
	}
	s.press(alice, alice, list.MsgID, buttonData(t, list, ui.ActEdit, ""))
	d := s.mustDraft(alice, alice)
	if d.Data.EditTodoID == 0 || d.Step != model.StepConfirm {
		t.Fatalf("draft %+v", d)
	}
	card := d.MsgID
	lc := s.locale(alice)
	if head := lc.T("ui.wizard.edit.head", lc.Num(1)); !strings.Contains(s.shown(alice, card).Msg.Text, head) {
		t.Fatalf("card %q", s.shown(alice, card).Msg.Text)
	}

	// Change only the time, to 11:30.
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavEdit)
	s.wpress(alice, alice, card, ui.ActWizEdit, ui.WizEditTime)
	s.wpress(alice, alice, card, ui.ActWizTime, ui.WizTimeType)
	s.wsay(alice, alice, "11:30", 0)
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavSave)
	s.noDraft(alice, alice)

	td := s.todoByNum(alice, 1)
	if td.Num != 1 || td.Text != "buy milk" || td.DueAt == nil || !td.DueAt.Equal(utc(2026, 10, 11, 11, 30)) {
		t.Fatalf("item after Save: %+v", td)
	}

	// The old time (10:00) passes without a reminder; 11:30 fires it once.
	s.advanceTo(utc(2026, 10, 11, 10, 0))
	s.advance(time.Minute)
	if n := len(reminders(s.send, alice)); n != 0 {
		t.Fatalf("reminder at the old time: %d", n)
	}
	s.advanceTo(utc(2026, 10, 11, 11, 30))
	got := reminders(s.send, alice)
	if len(got) != 1 || !strings.Contains(got[0].Msg.Text, "buy milk") {
		t.Fatalf("reminders at the new time: %+v", got)
	}
	s.advance(time.Hour)
	s.advanceTo(utc(2026, 10, 12, 12, 0))
	if n := len(reminders(s.send, alice)); n != 1 {
		t.Fatalf("reminders after the new time: %d, want 1", n)
	}
}

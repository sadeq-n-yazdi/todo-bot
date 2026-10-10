package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Scenario 1: add, remind, snooze, remind again, Done.
func TestAddRemindSnoozeDone(t *testing.T) {
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})

	s.say(alice, alice, "/add buy milk in 2m")
	if got := reminders(s.send, alice); len(got) != 0 {
		t.Fatalf("reminder sent before it was due: %+v", got)
	}

	s.advance(time.Minute)
	if got := reminders(s.send, alice); len(got) != 0 {
		t.Fatalf("reminder sent 1m early: %+v", got)
	}

	s.advance(time.Minute)
	got := reminders(s.send, alice)
	if len(got) != 1 {
		t.Fatalf("got %d reminders at 2m, want 1", len(got))
	}
	first := got[0]
	if !strings.Contains(first.Msg.Text, "buy milk") || strings.Contains(first.Msg.Text, "late") {
		t.Errorf("reminder text %q", first.Msg.Text)
	}
	// Done and the three snooze buttons are present.
	buttonData(t, first, ui.ActReminderDone, "")
	buttonData(t, first, ui.ActSnooze, ui.Snooze1h)
	buttonData(t, first, ui.ActSnooze, ui.SnoozeTomorrow)

	// Sending it again would be a duplicate: nothing is pending now.
	s.advance(5 * time.Minute)
	if n := len(reminders(s.send, alice)); n != 1 {
		t.Fatalf("a sent reminder was sent again: %d messages", n)
	}

	// Snooze 10m: the button message is edited, and the item stays open.
	s.press(alice, alice, first.MsgID, buttonData(t, first, ui.ActSnooze, ui.Snooze10m))
	s.advance(9 * time.Minute)
	if n := len(reminders(s.send, alice)); n != 1 {
		t.Fatalf("snoozed reminder fired after 9m: %d messages", n)
	}
	s.advance(time.Minute)
	got = reminders(s.send, alice)
	if len(got) != 2 {
		t.Fatalf("got %d reminders after the snooze, want 2", len(got))
	}
	second := got[1]
	if !strings.Contains(second.Msg.Text, "buy milk") {
		t.Errorf("second reminder text %q", second.Msg.Text)
	}
	if second.MsgID == first.MsgID {
		t.Error("the snoozed reminder reused the first message")
	}

	// Done on the second one: the message is edited and the item is done.
	s.press(alice, alice, second.MsgID, buttonData(t, second, ui.ActReminderDone, ""))
	last, _ := s.send.Last()
	if !last.Edit || last.MsgID != second.MsgID {
		t.Errorf("Done did not edit the reminder message: %+v", last)
	}
	if td := s.todoByNum(alice, 1); td.Status != model.TodoDone {
		t.Errorf("todo status %q, want done", td.Status)
	}

	s.advance(48 * time.Hour)
	if n := len(reminders(s.send, alice)); n != 2 {
		t.Errorf("reminders sent after Done: %d total, want 2", n)
	}
	if _, ok, err := s.svc.NextWakeup(context.Background()); err != nil || ok {
		t.Errorf("NextWakeup = ok %v, err %v; want nothing pending", ok, err)
	}
}

// Scenario 2: a daily 09:00 reminder in Europe/Berlin keeps firing at 09:00
// local time across the DST change on 2026-10-25 (03:00 CEST -> 02:00 CET).
// Firings are checked on the 24th (CEST) through the 27th (CET).
func TestDailyRecurrenceAcrossDST(t *testing.T) {
	berlin := mustLoc(t, "Europe/Berlin")
	clk := clocktest.New(utc(2026, 10, 23, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})

	s.say(alice, alice, "/tz Europe/Berlin")
	s.say(alice, alice, "/add standup every day at 9:00")

	var fired []time.Time
	for day := 24; day <= 27; day++ {
		before := len(reminders(s.send, alice))
		// 09:00 Berlin on this day; one minute earlier nothing may fire.
		at := time.Date(2026, 10, day, 9, 0, 0, 0, berlin)
		s.advanceTo(at.Add(-time.Minute))
		if n := len(reminders(s.send, alice)); n != before {
			t.Fatalf("day %d: fired before 09:00 local", day)
		}
		s.advanceTo(at)
		got := reminders(s.send, alice)
		if len(got) != before+1 {
			t.Fatalf("day %d: %d reminders, want %d", day, len(got), before+1)
		}
		if strings.Contains(got[len(got)-1].Msg.Text, "late") {
			t.Errorf("day %d: reminder marked late: %q", day, got[len(got)-1].Msg.Text)
		}
		fired = append(fired, clk.Now().In(berlin))
		// The next occurrence is stored at 09:00 local on the next day.
		td := s.todoByNum(alice, 1)
		if td.DueAt == nil {
			t.Fatalf("day %d: recurring todo has no due time", day)
		}
		want := time.Date(2026, 10, day+1, 9, 0, 0, 0, berlin)
		if !td.DueAt.Equal(want) {
			t.Errorf("day %d: next due %v, want %v", day, td.DueAt.In(berlin), want)
		}
	}
	for i, f := range fired {
		if f.Hour() != 9 || f.Minute() != 0 {
			t.Errorf("firing %d at %v, want 09:00 local", i, f)
		}
	}
	// The clocks went back on the night of the 24th to the 25th, so the same
	// local time is one hour later in UTC.
	if u := fired[0].UTC(); u.Hour() != 7 {
		t.Errorf("24th fired at %v UTC, want 07:00 (CEST)", u)
	}
	if u := fired[1].UTC(); u.Hour() != 8 {
		t.Errorf("25th fired at %v UTC, want 08:00 (CET)", u)
	}
}

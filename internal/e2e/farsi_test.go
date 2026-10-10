package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/clock/clocktest"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg/tgtest"
)

// hasASCIIDigit reports whether s shows a number in Western digits.
func hasASCIIDigit(s string) bool { return strings.ContainsAny(s, "0123456789") }

// Scenario 3: a Farsi + Jalali user in Asia/Tehran adds a rent reminder that
// repeats on the 1st of every Jalali month. It fires on 1 Aban 1405
// (2026-10-23) and on 1 Azar 1405 (2026-11-22), at 10:00 Tehran time, and
// everything the bot shows is in Farsi with Persian digits.
func TestFarsiJalaliMonthlyRent(t *testing.T) {
	tehran := mustLoc(t, "Asia/Tehran")
	clk := clocktest.New(utc(2026, 10, 10, 12, 0))
	s := newStack(t, dbURL(t.TempDir()), clk, stackOpts{})

	s.say(alice, alice, "/lang fa")
	s.say(alice, alice, "/calendar jalali")
	s.say(alice, alice, "/tz Asia/Tehran")
	s.say(alice, alice, "/add اجاره هر ماه ۱ام ساعت ۱۰")

	added, _ := s.send.Last()
	for _, want := range []string{"اجاره", "۱ آبان", "۱۰:۰۰"} {
		if !strings.Contains(added.Msg.Text, want) {
			t.Errorf("add reply %q lacks %q", added.Msg.Text, want)
		}
	}
	if hasASCIIDigit(added.Msg.Text) {
		t.Errorf("add reply shows Western digits: %q", added.Msg.Text)
	}
	td := s.todoByNum(alice, 1)
	if td.Recur != "monthly 1 10:00 jalali" {
		t.Fatalf("recurrence %q", td.Recur)
	}

	// 10:00 in Tehran is 06:30 UTC (no DST since 2022). One minute earlier
	// nothing is sent.
	aban1 := time.Date(2026, 10, 23, 10, 0, 0, 0, tehran)
	s.advanceTo(aban1.Add(-time.Minute))
	if n := len(reminders(s.send, alice)); n != 0 {
		t.Fatalf("fired a minute early: %d reminders", n)
	}
	s.advanceTo(aban1)
	got := reminders(s.send, alice)
	if len(got) != 1 {
		t.Fatalf("1 Aban: %d reminders, want 1", len(got))
	}
	checkFarsiReminder(t, got[0])

	// The next occurrence is 1 Azar, shown in the Jalali calendar.
	s.say(alice, alice, "/list")
	list, _ := s.send.Last()
	for _, want := range []string{"اجاره", "۱ آذر", "۱۰:۰۰", "🔁"} {
		if !strings.Contains(list.Msg.Text, want) {
			t.Errorf("list %q lacks %q", list.Msg.Text, want)
		}
	}
	if hasASCIIDigit(list.Msg.Text) {
		t.Errorf("list shows Western digits: %q", list.Msg.Text)
	}
	azar1 := time.Date(2026, 11, 22, 10, 0, 0, 0, tehran)
	if td := s.todoByNum(alice, 1); td.DueAt == nil || !td.DueAt.Equal(azar1) {
		t.Fatalf("next due %v, want %v", td.DueAt, azar1)
	}

	s.advanceTo(azar1.Add(-time.Minute))
	if n := len(reminders(s.send, alice)); n != 1 {
		t.Fatalf("fired early: %d reminders", n)
	}
	s.advanceTo(azar1)
	got = reminders(s.send, alice)
	if len(got) != 2 {
		t.Fatalf("1 Azar: %d reminders, want 2", len(got))
	}
	checkFarsiReminder(t, got[1])

	// And on to 1 Dey (2026-12-22).
	dey1 := time.Date(2026, 12, 22, 10, 0, 0, 0, tehran)
	if td := s.todoByNum(alice, 1); td.DueAt == nil || !td.DueAt.Equal(dey1) {
		t.Errorf("next due %v, want %v", td.DueAt, dey1)
	}
}

func checkFarsiReminder(t *testing.T, m tgtest.Sent) {
	t.Helper()
	if !strings.Contains(m.Msg.Text, "اجاره") || strings.Contains(m.Msg.Text, "تأخیر") {
		t.Errorf("reminder text %q", m.Msg.Text)
	}
	var labels []string
	for _, row := range m.Msg.Keyboard {
		for _, b := range row {
			labels = append(labels, b.Text)
		}
	}
	all := strings.Join(labels, " ")
	for _, want := range []string{"انجام شد", "۱۰ دقیقه", "۱ ساعت", "فردا"} {
		if !strings.Contains(all, want) {
			t.Errorf("buttons %q lack %q", all, want)
		}
	}
	if hasASCIIDigit(all) {
		t.Errorf("buttons show Western digits: %q", all)
	}
}

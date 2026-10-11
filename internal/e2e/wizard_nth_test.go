package e2e

import (
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// The wizard's "monthly on the Nth / last weekday" choices (#87): the item
// is created from the buttons and the reminder fires on the right day.

// pickDate opens the month grid of the date step on card, pages to the month
// of the Gregorian date (y, m, d) in the user's calendar, and picks the day.
func pickDate(t *testing.T, s *stack, lc i18n.Locale, card int, y int, m time.Month, d int) {
	t.Helper()
	s.wpress(alice, alice, card, ui.ActWizDate, ui.WizDateGrid)
	cur := lc.Cal.Split(s.clk.Now())
	want := lc.Cal.Split(time.Date(y, m, d, 12, 0, 0, 0, time.UTC))
	for cur.Year != want.Year || cur.Month != want.Month {
		cur.Year, cur.Month = calendar.AddMonths(cur.Year, cur.Month, 1)
		s.wpress(alice, alice, card, ui.ActWizMonth, ui.MonthArg(lc.Cal, cur.Year, cur.Month))
	}
	s.wpress(alice, alice, card, ui.ActWizDate, dayArg(y, m, d))
}

// wizardNth runs the wizard for alice with a date, a time and a repeat
// choice, and returns the saved item.
func wizardNth(t *testing.T, s *stack, text string, y int, m time.Month, d int, hhmm, repeat string) model.Todo {
	t.Helper()
	before := len(s.todos(alice))
	s.say(alice, alice, "/new")
	card := s.card(alice)
	s.say(alice, alice, text)
	pickDate(t, s, s.locale(alice), card, y, m, d)
	s.wpress(alice, alice, card, ui.ActWizTime, hhmm)
	s.wpress(alice, alice, card, ui.ActWizRepeat, repeat)
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavSave)
	if n := len(s.todos(alice)); n != before+1 {
		t.Fatalf("%d items, want %d", n, before+1)
	}
	return s.todoByNum(alice, before+1)
}

// 1 Aban 1405 is Friday 2026-10-23, so Aban's Fridays are 1, 8, 15, 22 and
// 29 Aban (2026-11-20), and 29 Aban is the last one. Azar 1405 starts on a
// Sunday; its last Friday is 27 Azar (2026-12-18).
func TestWizardLastFridayJalali(t *testing.T) {
	s := newWizardStack(t)
	s.say(alice, alice, "/lang fa")
	s.say(alice, alice, "/calendar jalali")

	td := wizardNth(t, s, "جلسه", 2026, 11, 20, "1500", ui.WizRepeatLast)
	if td.Recur != "monthly fri#last 15:00 jalali" {
		t.Fatalf("rule %q", td.Recur)
	}
	due := time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)
	if td.DueAt == nil || !td.DueAt.Equal(due) {
		t.Fatalf("due %v, want %v", td.DueAt, due)
	}

	s.advanceTo(due.Add(-time.Minute))
	if n := len(reminders(s.send, alice)); n != 0 {
		t.Fatalf("fired early: %d reminders", n)
	}
	s.advanceTo(due)
	if n := len(reminders(s.send, alice)); n != 1 {
		t.Fatalf("29 Aban: %d reminders, want 1", n)
	}
	next := time.Date(2026, 12, 18, 15, 0, 0, 0, time.UTC) // 27 Azar
	if td := s.todoByNum(alice, 1); td.DueAt == nil || !td.DueAt.Equal(next) {
		t.Fatalf("next due %v, want %v", td.DueAt, next)
	}
	s.advanceTo(next)
	if n := len(reminders(s.send, alice)); n != 2 {
		t.Fatalf("27 Azar: %d reminders, want 2", n)
	}
}

// The first Friday of Aban is offered as "1st Friday" and not as "last".
func TestWizardNthFridayJalaliOffersOnlyNth(t *testing.T) {
	s := newWizardStack(t)
	s.say(alice, alice, "/lang fa")
	s.say(alice, alice, "/calendar jalali")
	s.say(alice, alice, "/new")
	card := s.card(alice)
	s.say(alice, alice, "x")
	pickDate(t, s, s.locale(alice), card, 2026, 10, 23) // 1 Aban
	s.wpress(alice, alice, card, ui.ActWizTime, "1500")
	for _, row := range s.shown(alice, card).Msg.Keyboard { // "last" is not offered for the 1st
		for _, b := range row {
			if a, err := ui.Decode(b.Data); err == nil && a.Kind == ui.ActWizRepeat && a.Arg == ui.WizRepeatLast {
				t.Fatalf("last Friday offered for 1 Aban")
			}
		}
	}
	s.wpress(alice, alice, card, ui.ActWizRepeat, ui.WizRepeatNth)
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavSave)
	if td := s.todoByNum(alice, 1); td.Recur != "monthly fri#1 15:00 jalali" {
		t.Fatalf("rule %q", td.Recur)
	}
}

// October 2026: the 4th Friday is the 23rd, the last one is the 30th.
func TestWizardNthAndLastFridayGregorian(t *testing.T) {
	s := newWizardStack(t)
	s.say(alice, alice, "/tz UTC")

	fourth := wizardNth(t, s, "fourth", 2026, 10, 23, "1500", ui.WizRepeatNth)
	last := wizardNth(t, s, "last", 2026, 10, 30, "1500", ui.WizRepeatLast)
	if fourth.Recur != "monthly fri#4 15:00" || last.Recur != "monthly fri#last 15:00" {
		t.Fatalf("rules %q %q", fourth.Recur, last.Recur)
	}

	d23 := time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)
	d30 := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	s.advanceTo(d23.Add(-time.Minute))
	if n := len(reminders(s.send, alice)); n != 0 {
		t.Fatalf("fired early: %d", n)
	}
	s.advanceTo(d23)
	if n := len(reminders(s.send, alice)); n != 1 {
		t.Fatalf("23 Oct: %d reminders, want 1", n)
	}
	s.advanceTo(d30.Add(-time.Minute))
	if n := len(reminders(s.send, alice)); n != 1 {
		t.Fatalf("before 30 Oct: %d reminders, want 1", n)
	}
	s.advanceTo(d30)
	if n := len(reminders(s.send, alice)); n != 2 {
		t.Fatalf("30 Oct: %d reminders, want 2", n)
	}
	// Both repeat on the last Friday of November (the 27th) as 4th and last.
	nov27 := time.Date(2026, 11, 27, 15, 0, 0, 0, time.UTC)
	for num, name := range map[int]string{1: "fourth", 2: "last"} {
		if td := s.todoByNum(alice, num); td.DueAt == nil || !td.DueAt.Equal(nov27) {
			t.Errorf("%s: next due %v, want %v", name, td.DueAt, nov27)
		}
	}
}

// Editing the date of a "4th Friday" item to a Tuesday drops the choice.
func TestWizardDateEditDropsNthRepeat(t *testing.T) {
	s := newWizardStack(t)
	s.say(alice, alice, "/new")
	card := s.card(alice)
	s.say(alice, alice, "x")
	pickDate(t, s, s.locale(alice), card, 2026, 10, 23)
	s.wpress(alice, alice, card, ui.ActWizTime, "1500")
	s.wpress(alice, alice, card, ui.ActWizRepeat, ui.WizRepeatNth)
	s.wpress(alice, alice, card, ui.ActWizNav, ui.WizNavEdit)
	s.wpress(alice, alice, card, ui.ActWizEdit, ui.WizEditDate)
	pickDate(t, s, s.locale(alice), card, 2026, 10, 27) // a Tuesday
	if d := s.mustDraft(alice, alice); d.Data.Repeat != model.RepeatOnce || d.Step != model.StepTime {
		t.Fatalf("draft %+v", d)
	}
}

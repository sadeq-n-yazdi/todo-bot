package ui

import (
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

// Wizard helpers frozen by the contracts PR (#68).

func TestNewTaskLabel(t *testing.T) {
	en, fa := i18n.Locale{Lang: i18n.En}, i18n.Locale{Lang: i18n.Fa}
	if NewTaskLabel(en) != "➕ New task" || NewTaskLabel(fa) != "➕ کار جدید" {
		t.Fatalf("labels %q %q", NewTaskLabel(en), NewTaskLabel(fa))
	}
	for _, s := range []string{"➕ New task", "➕ کار جدید", "  ➕ New task \n"} {
		if !IsNewTaskLabel(s) {
			t.Errorf("IsNewTaskLabel(%q) = false", s)
		}
	}
	for _, s := range []string{"", "New task", "➕ new task", "buy milk"} {
		if IsNewTaskLabel(s) {
			t.Errorf("IsNewTaskLabel(%q) = true", s)
		}
	}
	if kb := NewTaskKeyboard(fa); len(kb) != 1 || len(kb[0]) != 1 || kb[0][0] != NewTaskLabel(fa) {
		t.Errorf("NewTaskKeyboard = %v", kb)
	}
	if b := NewTaskButton(en); b.Text != NewTaskLabel(en) || b.Data != "ws:0" {
		t.Errorf("NewTaskButton = %+v", b)
	}
}

func TestWizardArgs(t *testing.T) {
	d := calendar.Date{Year: 2026, Month: 3, Day: 9}
	if s := DateArg(d); s != "20260309" {
		t.Fatalf("DateArg = %q", s)
	}
	if got, ok := ParseDateArg("20260309"); !ok || got != d {
		t.Fatalf("ParseDateArg = %v %v", got, ok)
	}
	for _, s := range []string{WizDateNone, WizDateGrid, "20260230", ""} {
		if _, ok := ParseDateArg(s); ok {
			t.Errorf("ParseDateArg(%q) ok", s)
		}
	}
	if s := MonthArg(calendar.Jalali, 1405, 1); s != "j140501" {
		t.Fatalf("MonthArg = %q", s)
	}
	if c, y, m, ok := ParseMonthArg("g202612"); !ok || c != calendar.Gregorian || y != 2026 || m != 12 {
		t.Fatalf("ParseMonthArg = %v %d %d %v", c, y, m, ok)
	}
	if c, y, m, ok := ParseMonthArg(MonthArg(calendar.Jalali, 1404, 12)); !ok || c != calendar.Jalali || y != 1404 || m != 12 {
		t.Fatalf("ParseMonthArg(jalali) = %v %d %d %v", c, y, m, ok)
	}
	if s := TimeArg(9, 5); s != "0905" {
		t.Fatalf("TimeArg = %q", s)
	}
	if h, m, ok := ParseTimeArg("2359"); !ok || h != 23 || m != 59 {
		t.Fatalf("ParseTimeArg = %d %d %v", h, m, ok)
	}
	for _, s := range []string{WizTimeDefault, WizTimeType, "2400"} {
		if _, _, ok := ParseTimeArg(s); ok {
			t.Errorf("ParseTimeArg(%q) ok", s)
		}
	}
	for _, s := range WizardTimes {
		if _, err := time.Parse("15:04", s); err != nil {
			t.Errorf("WizardTimes %q: %v", s, err)
		}
	}
	if FirstWeekday(calendar.Jalali) != time.Saturday || FirstWeekday(calendar.Gregorian) != time.Monday {
		t.Error("FirstWeekday")
	}
}

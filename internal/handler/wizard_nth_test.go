package handler

import (
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/nlparse"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// Monthly on the Nth / last weekday in the wizard (#87).

func TestWizardNthRepeatRules(t *testing.T) {
	h := New(Deps{Parser: nlparse.Parser{DefaultHour: 9}})
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	greg := i18n.Locale{Lang: i18n.En, Cal: calendar.Gregorian}
	jal := i18n.Locale{Lang: i18n.Fa, Cal: calendar.Jalali}
	cases := []struct {
		name, date, repeat, want string
		lc                       i18n.Locale
	}{
		{"greg last", "2026-10-30", model.RepeatLastWeekday, "monthly fri#last 16:00", greg},
		{"greg 4th", "2026-10-23", model.RepeatNthWeekday, "monthly fri#4 16:00", greg},
		{"greg 1st", "2026-10-02", model.RepeatNthWeekday, "monthly fri#1 16:00", greg},
		// 29 Aban 1405 is the last Friday of Aban.
		{"jalali last", "2026-11-20", model.RepeatLastWeekday, "monthly fri#last 16:00 jalali", jal},
		// 1 Aban 1405 is a Friday.
		{"jalali 1st", "2026-10-23", model.RepeatNthWeekday, "monthly fri#1 16:00 jalali", jal},
	}
	for _, c := range cases {
		res, problem, err := h.buildResult(model.Draft{Data: model.DraftData{Text: "x", Date: c.date, Time: "16:00", Repeat: c.repeat}}, c.lc, time.UTC, now)
		if err != nil || problem != saveOK || res.Rule == nil || res.Rule.String() != c.want {
			t.Errorf("%s: %v %v %+v", c.name, err, problem, res.Rule)
		}
	}
	bad := []struct{ date, repeat string }{
		{"2026-10-23", model.RepeatLastWeekday}, // 23rd of a 31-day month
		{"2026-10-30", model.RepeatNthWeekday},  // the 5th Friday
	}
	for _, c := range bad {
		_, problem, err := h.buildResult(model.Draft{Data: model.DraftData{Text: "x", Date: c.date, Time: "16:00", Repeat: c.repeat}}, greg, time.UTC, now)
		if err != nil || problem != saveRepeatBad {
			t.Errorf("%v: problem %v err %v", c, problem, err)
		}
	}
}

func TestWizardNthRepeatPress(t *testing.T) {
	e := newWz(t)
	d := e.seed(wzGrp, model.StepRepeat, model.DraftData{Text: "x", Date: "2026-10-23", Time: "16:00"})
	// 23 Oct is the 4th Friday, not the last: "last" is refused, "nth" is taken.
	e.pressD(d, wzAlice, ui.ActWizRepeat, ui.WizRepeatLast)
	if got := e.draft(wzGrp, wzAlice); got.Step != model.StepRepeat || got.Data.Repeat != "" {
		t.Fatalf("last accepted: %+v", got)
	}
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizRepeat, ui.WizRepeatNth)
	got := e.draft(wzGrp, wzAlice)
	if got.Data.Repeat != model.RepeatNthWeekday || got.Step != model.StepAssignee {
		t.Fatalf("nth: %+v", got)
	}
}

func TestWizardDateEditClearsStaleNthRepeat(t *testing.T) {
	cases := []struct {
		name, from, repeat, to string
		keep                   bool
	}{
		{"nth to a Tuesday", "2026-10-23", model.RepeatNthWeekday, "20261027", false},
		{"nth to a 5th Friday", "2026-10-23", model.RepeatNthWeekday, "20261030", false},
		{"last to another Friday, not last", "2026-10-30", model.RepeatLastWeekday, "20261113", false},
		{"last to a Tuesday", "2026-10-30", model.RepeatLastWeekday, "20261027", false},
		{"nth to another valid Friday", "2026-10-23", model.RepeatNthWeekday, "20261113", true},
		{"last to the next last Friday", "2026-10-30", model.RepeatLastWeekday, "20261127", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newWz(t)
			d := e.seed(wzGrp, model.StepConfirm, model.DraftData{Text: "x", Date: c.from, Time: "16:00", Repeat: c.repeat})
			e.pressD(d, wzAlice, ui.ActWizEdit, ui.WizEditDate)
			e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizDate, c.to)
			got := e.draft(wzGrp, wzAlice)
			if got.Step != model.StepTime {
				t.Fatalf("step %s", got.Step)
			}
			if kept := got.Data.Repeat == c.repeat; kept != c.keep {
				t.Fatalf("repeat %q after the edit, keep=%v", got.Data.Repeat, c.keep)
			}
			// The edit walks on to the repeat step.
			e.pressD(got, wzAlice, ui.ActWizTime, "1600")
			if s := e.draft(wzGrp, wzAlice).Step; s != model.StepRepeat {
				t.Fatalf("step %s, want repeat", s)
			}
		})
	}
}

func TestWizardSaveNthRepeatEndToEnd(t *testing.T) {
	e := newWz(t)
	d := e.seed(wzGrp, model.StepRepeat, model.DraftData{Text: "pay rent", Date: "2026-10-30", Time: "16:00"})
	e.pressD(d, wzAlice, ui.ActWizRepeat, ui.WizRepeatLast)
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizAssign, ui.WizAssignNone)
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
	ts := e.todos(wzGrp)
	if len(ts) != 1 || ts[0].Recur != "monthly fri#last 16:00" {
		t.Fatalf("items %+v", ts)
	}
}

func TestWizardSaveStaleNthRepeatGoesBack(t *testing.T) {
	e := newWz(t)
	// A draft whose date no longer fits its repeat (for example written by
	// another node) is sent back to the repeat step, not saved.
	d := e.seed(wzGrp, model.StepConfirm, model.DraftData{Text: "x", Date: "2026-10-30", Time: "16:00", Repeat: model.RepeatNthWeekday})
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavSave)
	got := e.draft(wzGrp, wzAlice)
	if got.Step != model.StepRepeat || got.Data.Repeat != model.RepeatOnce || len(e.todos(wzGrp)) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestRepeatKindOfNthWeekdayRule(t *testing.T) {
	for rule, want := range map[string]string{
		"monthly fri#last 16:00":        model.RepeatLastWeekday,
		"monthly fri#2 16:00 jalali":    model.RepeatNthWeekday,
		"monthly 5 16:00":               model.RepeatMonthly,
		"monthly fri#last 16:00 jalali": model.RepeatLastWeekday,
	} {
		if k := repeatKind(rule); k != want {
			t.Errorf("repeatKind(%q) = %q, want %q", rule, k, want)
		}
	}
}

// A found "last Friday" rule is dropped for an explicit date: its kind is
// offered again only when the new date is a Friday that fits it.
func TestWizardFoundNthRuleWithExplicitDate(t *testing.T) {
	cases := []struct{ to, want string }{
		{"20261030", model.RepeatLastWeekday}, // the last Friday
		{"20261113", model.RepeatOnce},        // a Friday, not the last
		{"20261027", model.RepeatOnce},        // a Tuesday
	}
	for _, c := range cases {
		e := newWz(t)
		due := time.Date(2026, 10, 30, 16, 0, 0, 0, time.UTC)
		d := e.seed(wzGrp, model.StepDate, model.DraftData{
			Text: "x", OrigText: "x every last friday of the month 4pm", FoundText: "x",
			FoundDue: &due, FoundRule: "monthly fri#last 16:00", FoundTime: true,
		})
		e.pressD(d, wzAlice, ui.ActWizDate, c.to)
		if got := e.draft(wzGrp, wzAlice); got.Data.Repeat != c.want || got.Data.FoundRule != "" {
			t.Errorf("%s: %+v", c.to, got.Data)
		}
	}
	// Using the found date as it is keeps the rule unchanged.
	e := newWz(t)
	due := time.Date(2026, 10, 30, 16, 0, 0, 0, time.UTC)
	d := e.seed(wzGrp, model.StepFound, model.DraftData{
		Text: "x", OrigText: "x", FoundText: "x", FoundDue: &due, FoundRule: "monthly fri#last 16:00", FoundTime: true,
	})
	e.pressD(d, wzAlice, ui.ActWizNav, ui.WizNavUseFound)
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizAssign, ui.WizAssignNone)
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
	if ts := e.todos(wzGrp); len(ts) != 1 || ts[0].Recur != "monthly fri#last 16:00" {
		t.Fatalf("items %+v", ts)
	}
}

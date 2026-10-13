package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/ui"
)

// A calendar change between the card and Save must not change the saved
// repeat unseen (#96).

// calChangeSave walks the wizard to the confirm step in Gregorian with the
// repeat press, switches the user to Jalali, and presses Save.
func calChangeSave(t *testing.T, e *wzEnv, date, repeat string) {
	t.Helper()
	d := e.seed(wzGrp, model.StepRepeat, model.DraftData{Text: "x", Date: date, Time: "16:00"})
	e.pressD(d, wzAlice, ui.ActWizRepeat, repeat)
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizAssign, ui.WizAssignNone)
	if err := e.svc.SetCalendar(context.Background(), wzAlice, calendar.Jalali); err != nil {
		t.Fatal(err)
	}
	e.pressD(e.draft(wzGrp, wzAlice), wzAlice, ui.ActWizNav, ui.WizNavSave)
}

func TestWizardSaveAfterCalendarChange(t *testing.T) {
	cases := []struct {
		name, date, repeat string
	}{
		{"4th Friday", "2026-10-23", ui.WizRepeatNth},
		{"monthly", "2026-10-23", ui.WizRepeatMonthly},
		{"yearly", "2026-10-23", ui.WizRepeatYearly},
		{"last Friday", "2026-10-30", ui.WizRepeatLast},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newWz(t)
			calChangeSave(t, e, c.date, c.repeat)
			got := e.draft(wzGrp, wzAlice)
			if got.Step != model.StepRepeat || got.Data.Repeat != model.RepeatOnce || len(e.todos(wzGrp)) != 0 {
				t.Fatalf("draft %+v, items %+v", got, e.todos(wzGrp))
			}
			// The card carries the calendar note, not the "does not fit" one.
			txt := e.lastEdit().Msg.Text
			if !strings.Contains(txt, "Your calendar changed") || strings.Contains(txt, "does not fit the date") {
				t.Fatalf("card text %q", txt)
			}
			if got.Data.Cal != string(calendar.Jalali) {
				t.Fatalf("Cal %q after the redraw", got.Data.Cal)
			}
		})
	}
}

// A repeat that means the same in both calendars saves normally.
func TestWizardSaveAfterCalendarChangeSameRule(t *testing.T) {
	e := newWz(t)
	calChangeSave(t, e, "2026-10-23", ui.WizRepeatWeekly)
	ts := e.todos(wzGrp)
	if len(ts) != 1 || ts[0].Recur != "weekly fri 16:00" {
		t.Fatalf("items %+v", ts)
	}
}

func TestRepeatCalChangedSameCalendar(t *testing.T) {
	dd := model.DraftData{Repeat: model.RepeatMonthly, Cal: string(calendar.Jalali)}
	if repeatCalChanged(dd, calendar.Jalali) {
		t.Fatal("same calendar must mean no change")
	}
}

func TestRepeatCalChangedUnknownCal(t *testing.T) {
	dd := model.DraftData{Repeat: model.RepeatMonthly} // a draft from before the field
	if repeatCalChanged(dd, calendar.Jalali) {
		t.Fatal("no stored calendar must mean no check")
	}
}

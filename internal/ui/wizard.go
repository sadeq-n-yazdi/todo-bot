package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/model"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

// Task-creation wizard (#68, ARCHITECTURE §12): the contract between the
// handler (which runs the flow) and ui (which draws it). WizardCard and
// MonthGrid are implemented by the ui follow-up PR; the label and
// argument helpers below are final.

// WizardView is everything WizardCard needs to draw the card of one step.
type WizardView struct {
	Draft model.Draft
	// Members are the assignee step's choices besides "me / nobody":
	// allowlisted users with a username, the actor excluded, in order.
	Members []model.User
	// Grid, on the date step, shows the month grid of (Grid.Year,
	// Grid.Month) in the locale's calendar instead of the quick picks.
	// Grid.Day is ignored. nil = quick picks.
	Grid *calendar.Date
	// EditMenu, on the confirm step, shows the step chooser (one
	// ActWizEdit button per step, and ◀ Back to the summary) instead of
	// Save / Edit / Cancel.
	EditMenu bool
	// Note is an optional status line shown above the question, already
	// translated (e.g. "That time has passed").
	Note string
	// DefaultTime is TODO_DEFAULT_TIME as "HH:MM", for the Default button
	// and the summary of an item whose Data.Time is "".
	DefaultTime string
}

// WizardTimes are the time step's quick picks: morning, noon, afternoon,
// evening, night.
var WizardTimes = [...]string{"09:00", "12:00", "15:00", "18:00", "20:00"}

// WizardCard renders the wizard card for v.Draft.Step in lc's language,
// calendar and digits; loc and now are the user's zone and the current
// time. Every button's data comes from Encode with ID v.Draft.Version.
// The keyboard follows ARCHITECTURE §12: quick picks first, then
// ⏭ Skip / ◀ Back / ✖ Cancel where the step allows them.
//
// TODO(#68): implemented by the ui follow-up PR. Until then it returns
// the zero OutMessage.
func WizardCard(v WizardView, lc i18n.Locale, loc *time.Location, now time.Time) tg.OutMessage {
	_, _, _, _ = v, lc, loc, now
	return tg.OutMessage{}
}

// DayCell is one cell of a month grid.
type DayCell struct {
	// Day is the day of the month in the grid's calendar; 0 = padding
	// before the 1st or after the last day.
	Day int
	// Date is the cell's Gregorian civil date (zero for padding). It is
	// what an ActWizDate button carries (DateArg).
	Date calendar.Date
	// Past is true for days before today: shown, but not choosable.
	Past bool
}

// MonthGrid lays out month (year, month) of cal as weeks of 7 cells. A
// week starts on Saturday for Jalali and on Monday for Gregorian. today is
// the user's current Gregorian civil date; days before it are Past. Every
// row has exactly 7 cells, padded with Day 0 cells.
//
// TODO(#68): implemented by the ui follow-up PR. Until then it returns nil.
func MonthGrid(cal calendar.Kind, year, month int, today calendar.Date) [][]DayCell {
	_, _, _, _ = cal, year, month, today
	return nil
}

// FirstWeekday is the first column of a month grid in cal: Saturday for
// Jalali, Monday for Gregorian.
func FirstWeekday(cal calendar.Kind) time.Weekday {
	if cal == calendar.Jalali {
		return time.Saturday
	}
	return time.Monday
}

// ---- the "➕ New task" entry points ----

// NewTaskLabel is the label of the persistent reply-keyboard button, in
// lc's language.
func NewTaskLabel(lc i18n.Locale) string { return lc.T("ui.newtask.label") }

// IsNewTaskLabel reports whether text is the reply-keyboard button's label
// in any language (the keyboard may have been sent before /lang).
func IsNewTaskLabel(text string) bool {
	text = strings.TrimSpace(text)
	for _, l := range []i18n.Lang{i18n.En, i18n.Fa} {
		if text == NewTaskLabel(i18n.Locale{Lang: l}) {
			return true
		}
	}
	return false
}

// NewTaskKeyboard is the persistent reply keyboard for private chats
// (tg.OutMessage.ReplyKeyboard).
func NewTaskKeyboard(lc i18n.Locale) [][]string { return [][]string{{NewTaskLabel(lc)}} }

// NewTaskButton is the inline "➕ New task" button under /list.
func NewTaskButton(lc i18n.Locale) tg.Button {
	return button(NewTaskLabel(lc), Action{Kind: ActWizStart})
}

// ---- argument helpers (both directions, so ui and handler agree) ----

// DateArg is the ActWizDate argument for a Gregorian civil date.
func DateArg(d calendar.Date) string { return fmt.Sprintf("%04d%02d%02d", d.Year, d.Month, d.Day) }

// ParseDateArg parses a DateArg; ok=false for anything else (including
// WizDateNone, WizDateGrid and WizDateQuick).
func ParseDateArg(arg string) (calendar.Date, bool) {
	if !validDate(arg) {
		return calendar.Date{}, false
	}
	y, _ := strconv.Atoi(arg[:4])
	m, _ := strconv.Atoi(arg[4:6])
	d, _ := strconv.Atoi(arg[6:])
	return calendar.Date{Year: y, Month: m, Day: d}, true
}

// MonthArg is the ActWizMonth argument for month (year, month) of cal.
func MonthArg(cal calendar.Kind, year, month int) string {
	c := "g"
	if cal == calendar.Jalali {
		c = "j"
	}
	return fmt.Sprintf("%s%04d%02d", c, year, month)
}

// ParseMonthArg parses a MonthArg.
func ParseMonthArg(arg string) (cal calendar.Kind, year, month int, ok bool) {
	if !validMonth(arg) {
		return "", 0, 0, false
	}
	cal = calendar.Gregorian
	if arg[0] == 'j' {
		cal = calendar.Jalali
	}
	year, _ = strconv.Atoi(arg[1:5])
	month, _ = strconv.Atoi(arg[5:])
	return cal, year, month, true
}

// TimeArg is the ActWizTime argument for hh:mm.
func TimeArg(hh, mm int) string { return fmt.Sprintf("%02d%02d", hh, mm) }

// ParseTimeArg parses a TimeArg; ok=false for anything else (including
// WizTimeDefault and WizTimeType).
func ParseTimeArg(arg string) (hh, mm int, ok bool) {
	if !validHHMM(arg) {
		return 0, 0, false
	}
	hh, _ = strconv.Atoi(arg[:2])
	mm, _ = strconv.Atoi(arg[2:])
	return hh, mm, true
}

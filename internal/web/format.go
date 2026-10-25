package web

import (
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

// formatFull is due_full (docs/WEB-DESIGN.md §5.2): the absolute form of a
// date, with the weekday, the day, the month name, the year and the time,
// in the user's calendar, language and time zone. The relative form
// (due_text) is out of date in an offline view; this one is not.
//
//	en, Gregorian: Sun 25 Oct 2026 18:00
//	fa, Jalali:    یکشنبه ۳ آبان ۱۴۰۵ ۱۸:۰۰
func formatFull(t time.Time, lc i18n.Locale, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	t = t.In(loc)
	cal := lc.Cal
	if !cal.Valid() {
		cal = calendar.Gregorian
	}
	d := cal.Split(t)
	month := lc.MonthName(d.Month)
	if lc.Lang != i18n.Fa && cal == calendar.Gregorian {
		month = month[:3] // "Oct"; the other month names are used in full
	}
	return lc.WeekdayName(t.Weekday(), true) + " " + lc.Num(d.Day) + " " + month + " " +
		lc.Num(d.Year) + " " + lc.Digits(t.Format("15:04"))
}

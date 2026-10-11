package ui

import (
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/tg"
)

func civilKey(d calendar.Date) int { return d.Year*10000 + d.Month*100 + d.Day }

// monthGrid is the implementation of MonthGrid.
func monthGrid(cal calendar.Kind, year, month int, today calendar.Date) [][]DayCell {
	if !cal.Valid() {
		cal = calendar.Gregorian
	}
	n := cal.MonthLength(year, month)
	gy, gm, gd := cal.ToGregorian(calendar.Date{Year: year, Month: month, Day: 1})
	first := time.Date(gy, gm, gd, 0, 0, 0, 0, time.UTC)
	offset := (int(first.Weekday()) - int(FirstWeekday(cal)) + 7) % 7
	var rows [][]DayCell
	row := make([]DayCell, 0, 7)
	for i := 0; i < offset; i++ {
		row = append(row, DayCell{})
	}
	for day := 1; day <= n; day++ {
		y, m, d := first.AddDate(0, 0, day-1).Date() // calendar days, never 24h steps across DST
		g := calendar.Date{Year: y, Month: int(m), Day: d}
		row = append(row, DayCell{Day: day, Date: g, Past: civilKey(g) < civilKey(today)})
		if len(row) == 7 {
			rows = append(rows, row)
			row = make([]DayCell, 0, 7)
		}
	}
	if len(row) > 0 {
		for len(row) < 7 {
			row = append(row, DayCell{})
		}
		rows = append(rows, row)
	}
	return rows
}

// gridKeyboard is the month-grid view of the date step: weekday header,
// the weeks, then ◀ month ▶ and the way back to the quick picks.
func gridKeyboard(v WizardView, lc i18n.Locale, cal calendar.Kind, today calendar.Date) (text string, kb [][]tg.Button) {
	ver := v.Draft.Version
	g := *v.Grid
	text = lc.T("ui.wizard.q.grid", lc.MonthName(g.Month), lc.Num(g.Year))
	noop := func(label, arg string) tg.Button {
		return button(label, Action{Kind: ActWizNoop, ID: ver, Arg: arg})
	}
	first := FirstWeekday(cal)
	head := make([]tg.Button, 0, 7)
	for i := 0; i < 7; i++ {
		wd := time.Weekday((int(first) + i) % 7)
		head = append(head, noop(lc.T("ui.wizard.wd."+string(rune('0'+int(wd)))), ""))
	}
	kb = append(kb, head)
	for _, week := range monthGrid(cal, g.Year, g.Month, today) {
		r := make([]tg.Button, 0, 7)
		for _, c := range week {
			switch {
			case c.Day == 0:
				r = append(r, noop(" ", ""))
			case c.Past:
				r = append(r, noop(lc.Num(c.Day), WizNoopPast))
			default:
				r = append(r, button(lc.Num(c.Day), Action{Kind: ActWizDate, ID: ver, Arg: DateArg(c.Date)}))
			}
		}
		kb = append(kb, r)
	}
	// Every earlier day is past, so ◀ is not offered for the current month.
	cur := cal.Split(time.Date(today.Year, time.Month(today.Month), today.Day, 0, 0, 0, 0, time.UTC))
	var nav []tg.Button
	if g.Year*12+g.Month > cur.Year*12+cur.Month {
		py, pm := calendar.AddMonths(g.Year, g.Month, -1)
		nav = append(nav, button(lc.T("ui.wizard.btn.prev"), Action{Kind: ActWizMonth, ID: ver, Arg: MonthArg(cal, py, pm)}))
	}
	ny, nm := calendar.AddMonths(g.Year, g.Month, 1)
	nav = append(nav, button(lc.T("ui.wizard.btn.next"), Action{Kind: ActWizMonth, ID: ver, Arg: MonthArg(cal, ny, nm)}))
	kb = append(kb, nav)
	kb = append(kb, []tg.Button{
		button(lc.T("ui.wizard.btn.quick"), Action{Kind: ActWizDate, ID: ver, Arg: WizDateQuick}),
		button(lc.T("ui.wizard.btn.cancel"), Action{Kind: ActWizNav, ID: ver, Arg: WizNavCancel}),
	})
	return text, kb
}

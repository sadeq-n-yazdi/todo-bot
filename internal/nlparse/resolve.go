package nlparse

import (
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
)

func (p Parser) defaults() (int, int) {
	if p.DefaultHour == 0 && p.DefaultMinute == 0 {
		return 9, 0
	}
	return p.DefaultHour, p.DefaultMinute
}

// resolve turns a matched phrase into a Result (without Text).
func (p Parser) resolve(ph phrase, now time.Time, cal calendar.Kind) (Result, bool) {
	loc := now.Location()
	h, m := p.defaults()
	hasTime := false
	switch {
	case ph.tm != nil:
		h, m, hasTime = ph.tm.h, ph.tm.m, true
		if ph.tonight && h >= 1 && h < 12 {
			h += 12
		}
	case ph.tonight:
		h, m, hasTime = 20, 0, true
	}
	ny, nm, nd := now.Date()
	at := func(offset, hh, mm int) time.Time { return time.Date(ny, nm, nd+offset, hh, mm, 0, 0, loc) }

	var due time.Time
	var rule *recur.Rule
	switch {
	case ph.rec != nil:
		r := buildRule(*ph.rec, h, m, now, cal)
		due = firstOccurrence(&r, now)
		rule = &r
	case ph.rel != nil:
		switch ph.rel.u {
		case unitMinute:
			due = time.Date(ny, nm, nd, now.Hour(), now.Minute()+ph.rel.n, 0, 0, loc)
			hasTime = true
		case unitHour:
			due = time.Date(ny, nm, nd, now.Hour()+ph.rel.n, now.Minute(), 0, 0, loc)
			hasTime = true
		case unitDay:
			due = at(ph.rel.n, h, m)
		case unitWeek:
			due = at(7*ph.rel.n, h, m)
		}
	case ph.date != nil:
		var ok bool
		if due, ok = resolveDate(ph.date, now, h, m); !ok {
			return Result{}, false
		}
	default: // time of day only
		due = at(0, h, m)
		if !due.After(now) {
			due = at(1, h, m)
		}
	}
	return Result{Due: &due, Rule: rule, HasTime: hasTime}, true
}

func resolveDate(d *dateSpec, now time.Time, h, m int) (time.Time, bool) {
	loc := now.Location()
	ny, nm, nd := now.Date()
	switch d.kind {
	case dateOffset:
		return time.Date(ny, nm, nd+d.offset, h, m, 0, 0, loc), true
	case dateWeekday:
		delta := (int(d.wd) - int(now.Weekday()) + 7) % 7
		t := time.Date(ny, nm, nd+delta, h, m, 0, 0, loc)
		if delta == 0 && (d.next || !t.After(now)) {
			t = time.Date(ny, nm, nd+7, h, m, 0, 0, loc)
		}
		return t, true
	}
	if d.hasYear {
		return d.cal.At(calendar.Date{Year: d.y, Month: d.m, Day: d.d}, h, m, loc), true
	}
	// No year: the next occurrence of that day, possibly in a later year
	// (29 Feb and 30 Esfand need a leap year).
	cur := d.cal.Split(now).Year
	for y := cur; y < cur+9; y++ {
		if d.d > d.cal.MonthLength(y, d.m) {
			continue
		}
		if t := d.cal.At(calendar.Date{Year: y, Month: d.m, Day: d.d}, h, m, loc); t.After(now) {
			return t, true
		}
	}
	return time.Time{}, false
}

func buildRule(r recSpec, h, m int, now time.Time, cal calendar.Kind) recur.Rule {
	out := recur.Rule{Freq: r.freq, Cal: calendar.Gregorian, Hour: h, Minute: m}
	switch r.freq {
	case recur.EveryN:
		out.Interval = r.interval
	case recur.Weekly:
		out.Weekdays = append([]time.Weekday(nil), r.wds...)
		if len(out.Weekdays) == 0 {
			out.Weekdays = []time.Weekday{now.Weekday()}
		}
	case recur.Monthly:
		out.Cal = cal
		out.MonthDay = r.monthDay
		if out.MonthDay == 0 {
			out.MonthDay = cal.Split(now).Day
		}
	}
	return out
}

// firstOccurrence computes the first time rule r fires strictly after now.
// It is computed here (not with recur.Rule.Next) so the parser does not
// depend on the recurrence engine. For EveryN it also sets the rule's anchor.
func firstOccurrence(r *recur.Rule, now time.Time) time.Time {
	loc := now.Location()
	ny, nm, nd := now.Date()
	at := func(offset int) time.Time { return time.Date(ny, nm, nd+offset, r.Hour, r.Minute, 0, 0, loc) }
	var due time.Time
	switch r.Freq {
	case recur.Weekly:
		for off := 0; off <= 7; off++ {
			if t := at(off); t.After(now) && hasWeekday(r.Weekdays, t.Weekday()) {
				due = t
				break
			}
		}
	case recur.Monthly:
		cur := r.Cal.Split(now)
		for k := 0; k < 24; k++ {
			y, mo := calendar.AddMonths(cur.Year, cur.Month, k)
			day := min(r.MonthDay, r.Cal.MonthLength(y, mo))
			if t := r.Cal.At(calendar.Date{Year: y, Month: mo, Day: day}, r.Hour, r.Minute, loc); t.After(now) {
				due = t
				break
			}
		}
	default: // Daily, EveryN
		due = at(0)
		if !due.After(now) {
			due = at(1)
		}
		if r.Freq == recur.EveryN {
			r.Anchor = calendar.Date{Year: due.Year(), Month: int(due.Month()), Day: due.Day()}
		}
	}
	return due
}

func hasWeekday(list []time.Weekday, w time.Weekday) bool {
	for _, x := range list {
		if x == w {
			return true
		}
	}
	return false
}

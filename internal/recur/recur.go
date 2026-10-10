// Package recur implements recurrence rules evaluated on local calendar
// dates, so occurrences stay at the same wall-clock time across DST changes.
//
// Canonical string forms (Parse(r.String()) == r):
//
//	daily 09:00
//	weekly mon,wed 08:00        (weekdays = "weekly mon,tue,wed,thu,fri 09:30")
//	monthly 31 10:00            (clamped to the last day of shorter months)
//	monthly 1 10:00 jalali      (1st of every Jalali month)
//	every 3d 2026-10-10 09:00   (every N calendar days from the Gregorian anchor date)
//
// Only Monthly depends on the calendar: daily, weekly and every-N rules are
// calendar-independent (a day is a day), so their Cal is always Gregorian.
package recur

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
)

// Freq is the recurrence frequency.
type Freq string

const (
	Daily   Freq = "daily"
	Weekly  Freq = "weekly"
	Monthly Freq = "monthly"
	EveryN  Freq = "everyN"
)

// Rule describes when a recurring todo repeats.
type Rule struct {
	Freq     Freq
	Interval int            // EveryN only: number of days, >= 1
	Weekdays []time.Weekday // Weekly only: sorted, unique, non-empty
	MonthDay int            // Monthly only: 1..31, in Cal's months
	Cal      calendar.Kind  // Monthly only: Gregorian (default) or Jalali
	Hour     int            // 0..23, local time
	Minute   int            // 0..59
	Anchor   calendar.Date  // EveryN only: first occurrence, Gregorian date
}

// ErrInvalid is wrapped by Parse errors.
var ErrInvalid = errors.New("invalid recurrence rule")

// Weekday tokens in canonical order: Sunday-first, matching time.Weekday
// numbering. Parse sorts and de-duplicates, so String is stable.
var dayNames = [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

func parseClock(s string) (h, m int, err error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, invalid("time %q is not HH:MM", s)
	}
	h, e1 := strconv.Atoi(s[:2])
	m, e2 := strconv.Atoi(s[3:])
	if e1 != nil || e2 != nil || s[0] == '+' || s[0] == '-' || s[3] == '+' || s[3] == '-' {
		return 0, 0, invalid("time %q is not HH:MM", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, invalid("time %q out of range", s)
	}
	return h, m, nil
}

// Parse parses the canonical string form produced by String.
func Parse(s string) (Rule, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return Rule{}, invalid("empty rule")
	}
	r := Rule{Cal: calendar.Gregorian}
	var err error
	switch f[0] {
	case "daily":
		if len(f) != 2 {
			return Rule{}, invalid("want \"daily HH:MM\", got %q", s)
		}
		r.Freq = Daily
		r.Hour, r.Minute, err = parseClock(f[1])
	case "weekly":
		if len(f) != 3 {
			return Rule{}, invalid("want \"weekly mon,wed HH:MM\", got %q", s)
		}
		r.Freq = Weekly
		if r.Weekdays, err = parseWeekdays(f[1]); err != nil {
			return Rule{}, err
		}
		r.Hour, r.Minute, err = parseClock(f[2])
	case "monthly":
		if len(f) != 3 && len(f) != 4 {
			return Rule{}, invalid("want \"monthly D HH:MM [jalali]\", got %q", s)
		}
		r.Freq = Monthly
		d, e := strconv.Atoi(f[1])
		if e != nil || d < 1 || d > 31 {
			return Rule{}, invalid("month day %q not in 1..31", f[1])
		}
		r.MonthDay = d
		if len(f) == 4 {
			k := calendar.Kind(f[3])
			if !k.Valid() || k == calendar.Gregorian {
				return Rule{}, invalid("unknown calendar %q", f[3])
			}
			r.Cal = k
		}
		r.Hour, r.Minute, err = parseClock(f[2])
	case "every":
		if len(f) != 4 {
			return Rule{}, invalid("want \"every Nd YYYY-MM-DD HH:MM\", got %q", s)
		}
		r.Freq = EveryN
		n := strings.TrimSuffix(f[1], "d")
		if n == f[1] {
			return Rule{}, invalid("interval %q must end in d", f[1])
		}
		v, e := strconv.Atoi(n)
		if e != nil || v < 1 {
			return Rule{}, invalid("interval %q must be >= 1", f[1])
		}
		r.Interval = v
		t, e := time.Parse("2006-01-02", f[2])
		if e != nil {
			return Rule{}, invalid("anchor %q is not YYYY-MM-DD", f[2])
		}
		r.Anchor = calendar.Date{Year: t.Year(), Month: int(t.Month()), Day: t.Day()}
		r.Hour, r.Minute, err = parseClock(f[3])
	default:
		return Rule{}, invalid("unknown frequency %q", f[0])
	}
	if err != nil {
		return Rule{}, err
	}
	return r, nil
}

func parseWeekdays(s string) ([]time.Weekday, error) {
	var set [7]bool
	for _, tok := range strings.Split(s, ",") {
		found := false
		for i, n := range dayNames {
			if tok == n {
				set[i], found = true, true
			}
		}
		if !found {
			return nil, invalid("unknown weekday %q", tok)
		}
	}
	var out []time.Weekday
	for i, ok := range set {
		if ok {
			out = append(out, time.Weekday(i))
		}
	}
	return out, nil
}

// String returns the canonical form of r.
func (r Rule) String() string {
	clock := fmt.Sprintf("%02d:%02d", r.Hour, r.Minute)
	switch r.Freq {
	case Daily:
		return "daily " + clock
	case Weekly:
		names := make([]string, 0, len(r.Weekdays))
		for _, d := range r.Weekdays {
			names = append(names, dayNames[d])
		}
		return "weekly " + strings.Join(names, ",") + " " + clock
	case Monthly:
		s := fmt.Sprintf("monthly %d %s", r.MonthDay, clock)
		if r.Cal == calendar.Jalali {
			s += " jalali"
		}
		return s
	case EveryN:
		return fmt.Sprintf("every %dd %s %s", r.Interval, r.Anchor, clock)
	}
	return ""
}

// Next returns the first occurrence strictly after `after`, computed on
// calendar dates in loc and built with time.Date (never by adding Durations).
// Each local date yields at most one occurrence, so the repeated hour of an
// autumn DST change does not fire twice; a time in the spring gap is
// moved forward past the gap. It returns the zero time for an invalid
// rule.
func (r Rule) Next(after time.Time, loc *time.Location) time.Time {
	a := after.In(loc)
	y, m, d := a.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, time.UTC) // calendar-date arithmetic only
	at := func(day time.Time) time.Time {
		t := time.Date(day.Year(), day.Month(), day.Day(), r.Hour, r.Minute, 0, 0, loc)
		if t.Hour() != r.Hour || t.Minute() != r.Minute {
			// Spring gap: the wall time does not exist and Go resolved it
			// with the pre-transition offset (landing before the gap).
			// Move forward past the gap instead.
			t = time.Date(day.Year(), day.Month(), day.Day(), r.Hour+1, r.Minute, 0, 0, loc)
		}
		return t
	}
	if r.Freq == EveryN {
		if r.Interval < 1 {
			return time.Time{}
		}
		anchor := time.Date(r.Anchor.Year, time.Month(r.Anchor.Month), r.Anchor.Day, 0, 0, 0, 0, time.UTC)
		day := anchor
		if start.After(anchor) {
			diff := int(start.Sub(anchor) / (24 * time.Hour)) // UTC dates: exact multiple of 24h
			k := (diff + r.Interval - 1) / r.Interval
			day = anchor.AddDate(0, 0, k*r.Interval)
		}
		for i := 0; i < 3; i++ {
			if t := at(day); t.After(after) {
				return t
			}
			day = day.AddDate(0, 0, r.Interval)
		}
		return time.Time{}
	}
	cal := r.Cal
	if cal == "" {
		cal = calendar.Gregorian
	}
	for i := 0; i < 800; i++ {
		day := start.AddDate(0, 0, i)
		if r.matches(day, cal) {
			if t := at(day); t.After(after) {
				return t
			}
		}
	}
	return time.Time{}
}

// matches reports whether the calendar date `day` (a UTC-midnight stand-in
// for a civil date) is an occurrence date.
func (r Rule) matches(day time.Time, cal calendar.Kind) bool {
	switch r.Freq {
	case Daily:
		return true
	case Weekly:
		for _, w := range r.Weekdays {
			if w == day.Weekday() {
				return true
			}
		}
		return false
	case Monthly:
		cd := cal.Split(day)
		want := r.MonthDay
		if ml := cal.MonthLength(cd.Year, cd.Month); want > ml {
			want = ml
		}
		return cd.Day == want
	}
	return false
}

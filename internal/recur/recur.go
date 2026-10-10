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

var errNotImplemented = errors.New("recur: not implemented")

// Parse parses the canonical string form produced by String.
func Parse(s string) (Rule, error) {
	return Rule{}, errNotImplemented // TODO(#3)
}

// String returns the canonical form of r.
func (r Rule) String() string {
	return "" // TODO(#3)
}

// Next returns the first occurrence strictly after `after`, computed on
// calendar dates in loc and built with time.Date (never by adding Durations).
func (r Rule) Next(after time.Time, loc *time.Location) time.Time {
	return time.Time{} // TODO(#3)
}

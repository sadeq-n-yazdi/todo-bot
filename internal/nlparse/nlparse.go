// Package nlparse turns free text such as "buy milk tomorrow 18:00",
// "pills every day 9am", "خرید نان فردا ساعت ۱۸" or "اجاره هر ماه ۱ام" into
// item text, a due time and an optional recurrence rule.
//
// English and Farsi phrases are always both accepted, whatever the user's
// language; Persian/Arabic digits are normalised first (i18n.NormalizeDigits).
// cal decides how numeric dates ("15/7", "1405/07/15") and "every month on
// the Nth" are read; Jalali month names ("۱۵ مهر", "15 Mehr") are always Jalali
// and Gregorian month names always Gregorian. Dates are only recognised at
// the END of the input (optionally after "at"/"on"/"in"/"ساعت"), so
// "fix 2 bugs" stays plain text. Farsi "روزهای کاری" (workdays) means
// Saturday..Wednesday; English "weekdays" means Monday..Friday.
package nlparse

import (
	"errors"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
)

// Result is the outcome of parsing an /add line.
type Result struct {
	Text    string      // item text with the date phrase removed and trimmed
	Due     *time.Time  // nil when no date was found
	Rule    *recur.Rule // non-nil for recurring phrases; Due is then the first occurrence
	HasTime bool        // true when the input named a time of day
}

// Parser holds parsing defaults. The zero value uses 09:00.
type Parser struct {
	DefaultHour   int // used when a date has no time
	DefaultMinute int
}

// ErrNoDate is returned by ParseWhen when the input has no recognisable time.
var ErrNoDate = errors.New("no date or time found")

var errNotImplemented = errors.New("nlparse: not implemented")

// Parse parses input relative to now. now must already be in the user's
// location; returned times are in that location.
func (p Parser) Parse(input string, now time.Time, cal calendar.Kind) (Result, error) {
	return Result{}, errNotImplemented // TODO(#5)
}

// ParseWhen parses input that should consist only of a time phrase (reply to
// a "when?" prompt). It returns ErrNoDate if nothing matched.
func (p Parser) ParseWhen(input string, now time.Time, cal calendar.Kind) (time.Time, *recur.Rule, error) {
	return time.Time{}, nil, errNotImplemented // TODO(#5)
}

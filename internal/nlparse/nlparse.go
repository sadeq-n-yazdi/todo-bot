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
//
// # Implementation
//
// The parser is a small hand-written grammar rather than a wrapper around
// github.com/olebedev/when: the input is split into whitespace tokens, and
// the earliest token from which the rest of the input is, in full, one date
// phrase starts the phrase (so "monday meeting notes" has no date). A phrase
// is an optional recurrence head ("every monday and wednesday", "daily",
// "هر ۳ روز") followed by, or made of, a time of day, a date and/or a
// relative offset ("in 2h", "۲ ساعت دیگر"), in any order, with optional
// at/on/in connectors. A bare number is never a time; "at 5", "ساعت ۵",
// "5pm", "17:30" and "۵ عصر" are.
//
// Rules of thumb:
//   - A time with no date that already passed today means tomorrow.
//   - A date with no time uses Parser.DefaultHour/DefaultMinute. If that is
//     today and the slot has already passed ("today", "10 Oct" on 10 Oct,
//     "tonight" after 20:00), Due is today 23:59 and HasTime is false: the
//     service creates no reminder when HasTime is false and Due has already
//     passed.
//   - An explicit time on an explicit day ("today 9am" at 15:00) is kept as
//     given, in the past; only a bare time with no day rolls to tomorrow.
//   - "tonight 9am" is tomorrow 09:00; "tonight 9" and "tonight at 3" mean
//     21:00 and 15:00 (the latter only while still ahead).
//   - "in 2h" and "in 90m" are elapsed time (now.Add); "in 3 days" and "in 2
//     weeks" are calendar days at the default or given time.
//   - Three-letter weekdays (sun..sat) and the words daily/weekly/monthly are
//     a date phrase only after on/next/every/each, together with a time, or
//     as the whole ParseWhen input, so "watch the sun" and "send weekly"
//     stay text. Full weekday names always count at the end of the text.
//   - Phrases may start only within the last 16 tokens of the input.
//   - A bare weekday means its next occurrence, today included when the time
//     is still ahead; "next monday" is never today.
//   - A date without a year is its next occurrence (this or a later year).
//   - Numeric dates are day/month (or year-first when the year has four
//     digits) in cal, except that a four-digit year of 1700 or more is
//     Gregorian and one of 1200..1699 is Jalali, whatever cal is, so ISO
//     "2026-10-15" always means what it says.
//   - Daily, weekly and every-N rules are always Gregorian; a monthly rule
//     takes cal. Rule.Anchor of an every-N rule is the first occurrence.
//   - Result.Due is computed here, with calendar arithmetic, and is also set
//     for recurring phrases (the first occurrence).
package nlparse

import (
	"errors"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
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

// Parse parses input relative to now. now must already be in the user's
// location; returned times are in that location.
func (p Parser) Parse(input string, now time.Time, cal calendar.Kind) (Result, error) {
	toks := tokenize(i18n.NormalizeDigits(input))
	runes := []rune(input)
	for s := max(0, len(toks)-maxStartTokens); s < len(toks); s++ {
		res, ok := p.match(toks, s, now, cal, false)
		if !ok {
			continue
		}
		res.Text = cleanText(string(runes[:toks[s].start]))
		return res, nil
	}
	return Result{Text: strings.TrimSpace(input)}, nil
}

// ParseWhen parses input that should consist only of a time phrase (reply to
// a "when?" prompt). It returns ErrNoDate if nothing matched.
func (p Parser) ParseWhen(input string, now time.Time, cal calendar.Kind) (time.Time, *recur.Rule, error) {
	toks := tokenize(i18n.NormalizeDigits(input))
	if len(toks) > 0 {
		if res, ok := p.match(toks, 0, now, cal, true); ok {
			return *res.Due, res.Rule, nil
		}
	}
	return time.Time{}, nil, ErrNoDate
}

func (p Parser) match(toks []token, start int, now time.Time, cal calendar.Kind, lenient bool) (Result, bool) {
	if !cal.Valid() {
		cal = calendar.Gregorian
	}
	ph, ok := parseFrom(toks, start, cal, lenient)
	if !ok {
		return Result{}, false
	}
	return p.resolve(ph, now, cal)
}

// maxStartTokens bounds how far from the end a date phrase may start; it
// keeps parsing linear on long inputs.
const maxStartTokens = 16

func cleanText(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), " \t,;:-–—")
}

// Package nlparse turns free text such as "buy milk tomorrow 18:00",
// "pills every day 9am", "خرید نان فردا ساعت ۱۸" or "اجاره هر ماه ۱ام" into
// item text, a due time and an optional recurrence rule.
//
// English and Farsi phrases are always both accepted, whatever the user's
// language; Persian/Arabic digits are normalised first (i18n.NormalizeDigits).
// cal decides how numeric dates ("15/7", "1405/07/15") and "every month on
// the Nth" are read; Jalali month names ("۱۵ مهر", "15 Mehr") are always Jalali
// and Gregorian month names always Gregorian. Dates are only recognised at
// the END of the input or, failing that, at its START ("tomorrow 9am call
// mom"; a trailing phrase wins when both exist), ignoring @mentions and
// #tags, which stay in the text, so "fix 2 bugs" and "2 eggs" stay plain text. Farsi "روزهای کاری" (workdays) means
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
//   - A weekly or monthly repeat may be combined with a date and/or time, before
//     or after it ("call mom tomorrow 15:00 every week", "every month 1 Nov
//     pay rent"). "every week|month" (and weekly/monthly) take their weekday,
//     or their day of month in cal, from the date, and Due is that date. When
//     the repeat names its own days ("next monday every friday", "5 Nov every
//     month on the 10th") the repeat wins and the date only says where to
//     start: Due is the first occurrence on or after the date's day. Daily and
//     every-N repeats are not combined with a date ("tomorrow every day" is
//     "tomorrow" in the text and a plain daily rule).
//   - Result.Due is computed here, with calendar arithmetic, and is also set
//     for recurring phrases (the first occurrence).
package nlparse

import (
	"errors"
	"regexp"
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
	all := tokenize(i18n.NormalizeDigits(input))
	runes := []rune(input)
	// @mentions and #tags are not part of a date phrase, so they are hidden
	// from the grammar; those that fall inside or after the phrase are
	// appended to the text, in order, exactly as written.
	toks := make([]token, 0, len(all))
	for _, t := range all {
		if !isMention(t.key) {
			toks = append(toks, t)
		}
	}
	for s := max(0, len(toks)-maxStartTokens); s < len(toks); s++ {
		res, ok := p.match(toks, s, now, cal, false)
		if !ok {
			continue
		}
		text := string(runes[:toks[s].start])
		if cut := toks[s].start; cut < len(runes) {
			var kept []string
			for _, t := range all {
				if t.start >= cut && isMention(t.key) {
					kept = append(kept, string(runes[t.start:t.end]))
				}
			}
			if len(kept) > 0 {
				text = strings.TrimSpace(text) + " " + strings.Join(kept, " ")
			}
		}
		res.Text = cleanText(text)
		return res, nil
	}
	// No trailing phrase: try the longest phrase that starts the text (after
	// any leading @mentions/#tags); the rest of the text becomes the item text.
	if res, ok := p.leading(toks, all, runes, now, cal); ok {
		return res, nil
	}
	return Result{Text: strings.TrimSpace(input)}, nil
}

// leading matches the longest date phrase that begins at toks[0], leaving at
// least one token of item text. Mentions inside the phrase are kept, in order,
// between the text before the phrase and the text after it.
func (p Parser) leading(toks, all []token, runes []rune, now time.Time, cal calendar.Kind) (Result, bool) {
	for e := min(len(toks)-1, maxStartTokens); e >= 1; e-- {
		ph, ok := parseFrom(toks[:e], 0, validCal(cal), false)
		if !ok || bareWeekday(ph, e) || (ph.tm != nil && ph.tm.bareHour) {
			continue
		}
		res, ok := p.resolve(ph, now, validCal(cal))
		if !ok {
			continue
		}
		ps, pe := toks[0].start, toks[e-1].end
		parts := []string{strings.TrimSpace(string(runes[:ps]))}
		for _, t := range all {
			if t.start >= ps && t.end <= pe && isMention(t.key) {
				parts = append(parts, string(runes[t.start:t.end]))
			}
		}
		parts = append(parts, strings.TrimSpace(string(runes[pe:])))
		kept := parts[:0]
		for _, s := range parts {
			if s != "" {
				kept = append(kept, s)
			}
		}
		res.Text = cleanText(strings.Join(kept, " "))
		return res, true
	}
	return Result{}, false
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

func validCal(cal calendar.Kind) calendar.Kind {
	if !cal.Valid() {
		return calendar.Gregorian
	}
	return cal
}

// bareWeekday reports whether a one-token phrase is just a weekday name; at
// the start of the text that is too likely to be a title ("monday meeting
// notes"), so it needs a time, "next"/"on", etc. to count.
func bareWeekday(ph phrase, n int) bool {
	return n == 1 && ph.date != nil && ph.date.kind == dateWeekday && ph.tm == nil && ph.rel == nil && ph.rec == nil
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

// mentionRe matches "@wife", "#home" and "@bob_1": a sigil, a letter or underscore, then
// word characters, so "@3pm", "#3" and a bare "@" (the "at" connector) are not mentions.
var mentionRe = regexp.MustCompile(`^[@#][\p{L}_][\p{L}\p{N}_]*$`)

func isMention(key string) bool { return mentionRe.MatchString(key) }

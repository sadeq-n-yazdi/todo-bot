package nlparse

import (
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
)

func nthDay(cal calendar.Kind, nth int, wd time.Weekday, h, m int) *recur.Rule {
	return &recur.Rule{Freq: recur.MonthlyWeekday, Cal: cal, Weekdays: wk(wd), Nth: nth, Hour: h, Minute: m}
}

// Phrases that are not "of every month" keep the meaning they had before the
// Nth-weekday rule existed. These rows were recorded before the grammar
// changed; they pin today's behaviour and do not endorse it (the text keeps a
// stray "last"). Default now: Saturday 2026-10-10 08:15.
var nthPinnedRows = []row{
	{in: "x last friday", wantText: "x last", wantDue: "2026-10-16 09:00"},
	{in: "x every last friday", wantText: "x every last", wantDue: "2026-10-16 09:00"},
	{in: "x first monday", wantText: "x first", wantDue: "2026-10-12 09:00"},
	{in: "x next friday", wantText: "x", wantDue: "2026-10-16 09:00"},
	{in: "last friday", wantText: "last", wantDue: "2026-10-16 09:00"},
	{in: "first monday", wantText: "first", wantDue: "2026-10-12 09:00"},
	{in: "x last friday 4pm", wantText: "x last", wantDue: "2026-10-16 16:00", wantTime: true},
}

func TestNthWeekdayPinned(t *testing.T) { runRows(t, nthPinnedRows) }

// Default now: Saturday 2026-10-10 08:15 (18 Mehr 1405). October 2026 Fridays
// are 2, 9, 16, 23, 30. 1 Aban 1405 is Friday 23 Oct, so the last Friday of
// Aban is 29 Aban (20 Nov).
var nthRows = []row{
	// ---- English ----
	{in: "x every last friday of the month 4pm", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "x last friday of every month at 16:00", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "x every month on the last friday", wantText: "x", wantDue: "2026-10-30 09:00", wantRule: nthDay(greg, recur.Last, time.Friday, 9, 0)},
	{in: "x monthly on the last friday", wantText: "x", wantDue: "2026-10-30 09:00", wantRule: nthDay(greg, recur.Last, time.Friday, 9, 0)},
	{in: "x on the first monday of each month", wantText: "x", wantDue: "2026-11-02 09:00", wantRule: nthDay(greg, 1, time.Monday, 9, 0)},
	{in: "x every 2nd tuesday of the month", wantText: "x", wantDue: "2026-10-13 09:00", wantRule: nthDay(greg, 2, time.Tuesday, 9, 0)},
	{in: "x every third monday of the month", wantText: "x", wantDue: "2026-10-19 09:00", wantRule: nthDay(greg, 3, time.Monday, 9, 0)},
	{in: "x every 4th monday of every month", wantText: "x", wantDue: "2026-10-26 09:00", wantRule: nthDay(greg, 4, time.Monday, 9, 0)},
	{in: "x every friday of last week of month at 4pm", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "x every friday of the last week of the month at 4pm", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "x every friday in the last week of the month at 4pm", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "x every friday in last week of month at 4pm", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "x 4pm every last friday of the month", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "every last fri of the month 4pm x", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "x every last friday of the month 4pm @wife", wantText: "x @wife", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	// the time of the first due has passed: next month
	{in: "x every last friday of the month 4pm", now: "2026-10-30 17:00", wantText: "x", wantDue: "2026-11-27 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "x every last friday of the month 4pm", now: "2026-10-30 08:00", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	// a 4th Friday is not the last when the month has five
	{in: "x every 4th friday of the month", now: "2026-10-24 08:00", wantText: "x", wantDue: "2026-11-27 09:00", wantRule: nthDay(greg, 4, time.Friday, 9, 0)},
	// combined with a date: the rule names its own days and wins
	{in: "x tomorrow last friday of every month", wantText: "x", wantDue: "2026-10-30 09:00", wantRule: nthDay(greg, recur.Last, time.Friday, 9, 0)},
	{in: "x 5 Nov every month on the last friday", wantText: "x", wantDue: "2026-11-27 09:00", wantRule: nthDay(greg, recur.Last, time.Friday, 9, 0)},
	// ---- Farsi ----
	{in: "x آخرین جمعه هر ماه ساعت ۴ عصر", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true, wantRule: nthDay(greg, recur.Last, time.Friday, 16, 0)},
	{in: "x جمعه آخر هر ماه", wantText: "x", wantDue: "2026-10-30 09:00", wantRule: nthDay(greg, recur.Last, time.Friday, 9, 0)},
	{in: "x هر ماه آخرین جمعه", wantText: "x", wantDue: "2026-10-30 09:00", wantRule: nthDay(greg, recur.Last, time.Friday, 9, 0)},
	{in: "x اولین دوشنبه هر ماه", wantText: "x", wantDue: "2026-11-02 09:00", wantRule: nthDay(greg, 1, time.Monday, 9, 0)},
	{in: "x دومین دوشنبه هر ماه", wantText: "x", wantDue: "2026-10-12 09:00", wantRule: nthDay(greg, 2, time.Monday, 9, 0)},
	{in: "x سومین دوشنبه هر ماه", wantText: "x", wantDue: "2026-10-19 09:00", wantRule: nthDay(greg, 3, time.Monday, 9, 0)},
	{in: "x چهارمین دوشنبه هر ماه", wantText: "x", wantDue: "2026-10-26 09:00", wantRule: nthDay(greg, 4, time.Monday, 9, 0)},
	{in: "x سه\u200cشنبه دوم هر ماه ساعت ۸ صبح", now: "2026-10-10 06:00", wantText: "x", wantDue: "2026-10-13 08:00", wantTime: true, wantRule: nthDay(greg, 2, time.Tuesday, 8, 0)},
	{in: "x ۲ام دوشنبه هر ماه", wantText: "x", wantDue: "2026-10-12 09:00", wantRule: nthDay(greg, 2, time.Monday, 9, 0)},
	{in: "x every ۲nd monday of the month", wantText: "x", wantDue: "2026-10-12 09:00", wantRule: nthDay(greg, 2, time.Monday, 9, 0)},
	// ---- Jalali: the month is the user's calendar ----
	{in: "x every last friday of the month", cal: jal, wantText: "x", wantDue: "2026-10-16 09:00", wantRule: nthDay(jal, recur.Last, time.Friday, 9, 0)}, // 24 Mehr
	{in: "x آخرین جمعه هر ماه ساعت ۴ عصر", cal: jal, wantText: "x", wantDue: "2026-10-16 16:00", wantTime: true, wantRule: nthDay(jal, recur.Last, time.Friday, 16, 0)},
	{in: "x جمعه آخر هر ماه", cal: jal, wantText: "x", wantDue: "2026-10-16 09:00", wantRule: nthDay(jal, recur.Last, time.Friday, 9, 0)},
	{in: "x every last friday of the month", cal: jal, now: "2026-10-17 08:15", wantText: "x", wantDue: "2026-11-20 09:00", wantRule: nthDay(jal, recur.Last, time.Friday, 9, 0)}, // 29 Aban
	{in: "x every first friday of the month", cal: jal, now: "2026-10-17 08:15", wantText: "x", wantDue: "2026-10-23 09:00", wantRule: nthDay(jal, 1, time.Friday, 9, 0)},         // 1 Aban
	// Esfand 1405 has 29 days; its last Friday is the 28th (19 Mar 2027)
	{in: "x every last friday of the month", cal: jal, now: "2027-02-25 08:15", wantText: "x", wantDue: "2027-03-19 09:00", wantRule: nthDay(jal, recur.Last, time.Friday, 9, 0)},
	// the Gregorian user gets the Gregorian month for the same words
	{in: "x every last friday of the month", cal: greg, now: "2026-10-17 08:15", wantText: "x", wantDue: "2026-10-30 09:00", wantRule: nthDay(greg, recur.Last, time.Friday, 9, 0)},
}

func TestNthWeekday(t *testing.T) { runRows(t, nthRows) }

// Every Nth-weekday row yields a Due that is not in the past, is an
// occurrence of its own rule and falls on the rule's weekday.
func TestNthWeekdayDueIsAnOccurrence(t *testing.T) {
	checkDueRows(t, nthRows)
	for _, r := range nthRows {
		cal := r.cal
		if cal == "" {
			cal = greg
		}
		got, _ := Parser{}.Parse(r.in, parseNow(t, r.now, r.tz), cal)
		if got.Due == nil || got.Rule == nil || got.Due.Weekday() != got.Rule.Weekdays[0] {
			t.Errorf("%q: due %v rule %+v", r.in, got.Due, got.Rule)
		}
	}
}

// Forms that need a repeat word or a valid ordinal never give this rule.
func TestNthWeekdayNotMatched(t *testing.T) {
	for _, in := range []string{
		"x every 5th friday of the month",
		"x every 0th friday of the month",
		"x every 2 tuesday of the month",
		"x every last week of the month",
		"x on the first monday of march",
	} {
		got, _ := Parser{}.Parse(in, parseNow(t, "", ""), greg)
		if got.Rule != nil && got.Rule.Freq == recur.MonthlyWeekday {
			t.Errorf("%q: unexpected rule %+v", in, got.Rule)
		}
	}
}

func TestParseWhenNthWeekday(t *testing.T) {
	due, rule, err := Parser{}.ParseWhen("last friday of every month at 16:00", parseNow(t, "", ""), greg)
	if err != nil || due.Format(layout) != "2026-10-30 16:00" || rule == nil || rule.Nth != recur.Last {
		t.Fatalf("due %v rule %+v err %v", due, rule, err)
	}
}

// Every Freq switch must know MonthlyWeekday; firstOccurrence would
// otherwise treat it as daily.
func TestNthWeekdayFreqSwitches(t *testing.T) {
	now := parseNow(t, "", "")
	r := recur.Rule{Freq: recur.MonthlyWeekday, Cal: greg, Weekdays: wk(time.Friday), Nth: recur.Last, Hour: 16}
	if got := firstOccurrence(&r, now); got.Format(layout) != "2026-10-30 16:00" {
		t.Errorf("firstOccurrence = %s", got.Format(layout))
	}
	spec := recSpec{freq: recur.MonthlyWeekday, wds: wk(time.Monday), nth: 2}
	if !explicit(spec) {
		t.Error("explicit(MonthlyWeekday) = false")
	}
	if got := buildDatedRule(spec, 9, 0, now, jal); got.Freq != recur.MonthlyWeekday || got.Nth != 2 || got.Cal != jal || len(got.Weekdays) != 1 || got.Weekdays[0] != time.Monday {
		t.Errorf("buildDatedRule = %+v", got)
	}
	if got := buildRule(spec, 9, 0, now, jal); got.Freq != recur.MonthlyWeekday || got.Nth != 2 || got.Cal != jal {
		t.Errorf("buildRule = %+v", got)
	}
	ph, ok := parseFrom(tokenize("last friday of every month 4pm"), 0, greg, false)
	if !ok || ph.rec == nil || ph.rec.freq != recur.MonthlyWeekday {
		t.Errorf("parseFrom: ok=%v %+v", ok, ph.rec)
	}
	ph, ok = parseFrom(tokenize("tomorrow last friday of every month"), 0, greg, false)
	if !ok || ph.date == nil || ph.rec == nil {
		t.Errorf("parseFrom with a date (allow-list): ok=%v", ok)
	}
}

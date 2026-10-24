package nlparse

import (
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
)

// One-off "Nth/last weekday of this or next month". These rows give a Due and
// no rule. Default now: Saturday 2026-10-10 08:15 (18 Mehr 1405). October 2026
// Fridays are 2, 9, 16, 23, 30; November Fridays are 6, 13, 20, 27. 24 Mehr is
// 16 Oct and the last Friday of Mehr; 29 Aban is 20 Nov.
var monthWeekdayRows = []row{
	// ---- English, Gregorian ----
	{in: "x last friday of the month", wantText: "x", wantDue: "2026-10-30 09:00"},
	{in: "x the last friday of this month", wantText: "x", wantDue: "2026-10-30 09:00"},
	{in: "x on the last friday of this month", wantText: "x", wantDue: "2026-10-30 09:00"},
	{in: "last friday of the month x", wantText: "x", wantDue: "2026-10-30 09:00"},
	{in: "x last friday in the month", wantText: "x", wantDue: "2026-10-30 09:00"},
	{in: "x last fri of the month", wantText: "x", wantDue: "2026-10-30 09:00"},
	{in: "x last friday of the month at 4pm", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true},
	{in: "x 4pm last friday of the month", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true},
	{in: "x last friday of the month at 16:00 @wife", wantText: "x @wife", wantDue: "2026-10-30 16:00", wantTime: true},
	{in: "x first monday of next month", wantText: "x", wantDue: "2026-11-02 09:00"},
	{in: "x the first monday of next month at 8:30", wantText: "x", wantDue: "2026-11-02 08:30", wantTime: true},
	{in: "x first monday of the month", wantText: "x", wantDue: "2026-11-02 09:00"}, // 5 Oct has passed
	{in: "x first monday of this month", wantText: "x", wantDue: "2026-11-02 09:00"},
	{in: "x 2nd tuesday of the month", wantText: "x", wantDue: "2026-10-13 09:00"},
	{in: "x second tuesday of the month", wantText: "x", wantDue: "2026-10-13 09:00"},
	{in: "x 2nd tuesday of next month", wantText: "x", wantDue: "2026-11-10 09:00"},
	{in: "x last friday of next month", wantText: "x", wantDue: "2026-11-27 09:00"},
	{in: "x fourth monday of the month", wantText: "x", wantDue: "2026-10-26 09:00"},
	// the 4th Friday is not the last in a 5-Friday month
	{in: "x 4th friday of the month", wantText: "x", wantDue: "2026-10-23 09:00"},
	// today is that day
	{in: "x last friday of the month at 4pm", now: "2026-10-30 08:00", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true},
	{in: "x last friday of the month at 4pm", now: "2026-10-30 17:00", wantText: "x", wantDue: "2026-11-27 16:00", wantTime: true},
	{in: "x last friday of the month at 4pm", now: "2026-10-30 16:00", wantText: "x", wantDue: "2026-11-27 16:00", wantTime: true},
	{in: "x last friday of the month", now: "2026-10-30 10:00", wantText: "x", wantDue: "2026-10-30 23:59"}, // today wins with no time
	{in: "x آخرین جمعه این ماه", now: "2026-10-30 10:00", wantText: "x", wantDue: "2026-10-30 23:59"},
	{in: "x آخرین جمعه این ماه", cal: jal, now: "2026-10-16 10:00", wantText: "x", wantDue: "2026-10-16 23:59"}, // 24 Mehr
	{in: "x last friday of the month", cal: jal, now: "2026-10-16 10:00", wantText: "x", wantDue: "2026-10-16 23:59"},
	{in: "x last friday of next month", now: "2026-10-30 10:00", wantText: "x", wantDue: "2026-11-27 09:00"},
	{in: "x last friday of the month", now: "2026-10-30 08:00", wantText: "x", wantDue: "2026-10-30 09:00"},
	// this month's occurrence has passed
	{in: "x last friday of the month", now: "2026-10-31 08:00", wantText: "x", wantDue: "2026-11-27 09:00"},
	{in: "x first friday of this month", now: "2026-10-20 08:00", wantText: "x", wantDue: "2026-11-06 09:00"},
	// year wrap
	{in: "x first monday of next month", now: "2026-12-28 08:00", wantText: "x", wantDue: "2027-01-04 09:00"},
	{in: "x first monday of the month", now: "2026-12-28 08:00", wantText: "x", wantDue: "2027-01-04 09:00"},
	// "next month" is never this month, even when this month's day is ahead
	{in: "x last friday of next month", now: "2026-10-01 08:00", wantText: "x", wantDue: "2026-11-27 09:00"},
	// ---- Farsi, Gregorian user ----
	{in: "x آخرین جمعه این ماه", wantText: "x", wantDue: "2026-10-30 09:00"},
	{in: "x جمعه آخر ماه", wantText: "x", wantDue: "2026-10-30 09:00"},
	{in: "x اولین دوشنبه ماه بعد", wantText: "x", wantDue: "2026-11-02 09:00"},
	{in: "x اولین دوشنبه ماه آینده", wantText: "x", wantDue: "2026-11-02 09:00"},
	{in: "x دوشنبه اول ماه بعد", wantText: "x", wantDue: "2026-11-02 09:00"},
	{in: "x آخرین جمعه این ماه ساعت ۴ عصر", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true},
	{in: "x آخرین جمعه ماه ساعت ۴ عصر", wantText: "x", wantDue: "2026-10-30 16:00", wantTime: true},
	{in: "x دومین سه\u200cشنبه این ماه", wantText: "x", wantDue: "2026-10-13 09:00"},
	{in: "x ۲مین سه\u200cشنبه این ماه", wantText: "x", wantDue: "2026-10-13 09:00"},
	{in: "x آخرین جمعه این ماه ساعت ۴ عصر", now: "2026-10-30 17:00", wantText: "x", wantDue: "2026-11-27 16:00", wantTime: true},
	// ---- Jalali user: the month is the Jalali month ----
	{in: "x last friday of the month", cal: jal, wantText: "x", wantDue: "2026-10-16 09:00"}, // 24 Mehr
	{in: "x آخرین جمعه این ماه", cal: jal, wantText: "x", wantDue: "2026-10-16 09:00"},
	{in: "x جمعه آخر ماه", cal: jal, wantText: "x", wantDue: "2026-10-16 09:00"},
	{in: "x last friday of next month", cal: jal, wantText: "x", wantDue: "2026-11-20 09:00"}, // 29 Aban
	{in: "x اولین دوشنبه ماه بعد", cal: jal, wantText: "x", wantDue: "2026-10-26 09:00"},      // 4 Aban
	{in: "x first monday of next month", cal: jal, wantText: "x", wantDue: "2026-10-26 09:00"},
	{in: "x 2nd tuesday of the month", cal: jal, wantText: "x", wantDue: "2026-11-03 09:00"},  // 14 Mehr passed: 12 Aban
	{in: "x first friday of the month", cal: jal, wantText: "x", wantDue: "2026-10-23 09:00"}, // 3 Mehr passed: 1 Aban
	// today is the last Friday of Mehr (24 Mehr = 16 Oct)
	{in: "x آخرین جمعه این ماه ساعت ۴ عصر", cal: jal, now: "2026-10-16 08:00", wantText: "x", wantDue: "2026-10-16 16:00", wantTime: true},
	{in: "x آخرین جمعه این ماه ساعت ۴ عصر", cal: jal, now: "2026-10-16 17:00", wantText: "x", wantDue: "2026-11-20 16:00", wantTime: true},
	{in: "x last friday of the month at 4pm", cal: jal, now: "2026-10-16 17:00", wantText: "x", wantDue: "2026-11-20 16:00", wantTime: true},
	// the same words differ by calendar
	{in: "x last friday of the month", cal: jal, now: "2026-10-17 08:15", wantText: "x", wantDue: "2026-11-20 09:00"},
	{in: "x last friday of the month", cal: greg, now: "2026-10-17 08:15", wantText: "x", wantDue: "2026-10-30 09:00"},
	// Esfand 1405 has 29 days; its last Friday is the 28th (19 Mar 2027)
	{in: "x last friday of the month", cal: jal, now: "2027-02-25 08:15", wantText: "x", wantDue: "2027-03-19 09:00"},
	{in: "x جمعه آخر ماه", cal: jal, now: "2027-02-25 08:15", wantText: "x", wantDue: "2027-03-19 09:00"},
	// Esfand 1403 has 30 days; its last Thursday is the 30th (20 Mar 2025)
	{in: "x last thursday of the month", cal: jal, now: "2025-03-01 08:15", wantText: "x", wantDue: "2025-03-20 09:00"},
	// next month from Esfand is Farvardin of the next year: 1 Farvardin 1406 is Sun 21 Mar 2027, so the first Saturday is 6 Farvardin
	{in: "x first saturday of next month", cal: jal, now: "2027-02-25 08:15", wantText: "x", wantDue: "2027-03-27 09:00"},
	{in: "x اولین شنبه ماه بعد", cal: jal, now: "2027-02-25 08:15", wantText: "x", wantDue: "2027-03-27 09:00"},
	// Esfand's last Friday has passed: last Friday of Farvardin 1406 is the 27th (16 Apr 2027)
	{in: "x last friday of the month at 9am", cal: jal, now: "2027-03-19 17:00", wantText: "x", wantDue: "2027-04-16 09:00", wantTime: true},
}

func TestMonthWeekday(t *testing.T) { runRows(t, monthWeekdayRows) }

// Every row is a real occurrence: not in the past, with no rule.
func TestMonthWeekdayDue(t *testing.T) {
	for _, r := range monthWeekdayRows {
		cal := r.cal
		if cal == "" {
			cal = greg
		}
		now := parseNow(t, r.now, r.tz)
		got, _ := Parser{}.Parse(r.in, now, cal)
		if got.Due == nil || got.Rule != nil || !got.Due.After(now) {
			t.Errorf("%q: due %v rule %+v", r.in, got.Due, got.Rule)
		}
	}
}

// Month words that do not name this or next month, and bad ordinals, give no
// such date.
func TestMonthWeekdayNotMatched(t *testing.T) {
	for _, in := range []string{
		"x 5th friday of the month",
		"x 2 tuesday of the month",
		"x last week of the month",
		"x last friday of the year",
		"x last friday of march",
	} {
		got, _ := Parser{}.Parse(in, parseNow(t, "", ""), greg)
		if got.Rule != nil || (got.Due != nil && got.Due.Format(layout) == "2026-10-30 09:00") {
			t.Errorf("%q: unexpected %v %+v", in, got.Due, got.Rule)
		}
	}
}

func TestParseWhenMonthWeekday(t *testing.T) {
	due, rule, err := Parser{}.ParseWhen("last friday of the month at 16:00", parseNow(t, "", ""), greg)
	if err != nil || rule != nil || due.Format(layout) != "2026-10-30 16:00" {
		t.Fatalf("due %v rule %+v err %v", due, rule, err)
	}
	due, rule, err = Parser{}.ParseWhen("جمعه آخر ماه", parseNow(t, "", ""), jal)
	if err != nil || rule != nil || due.Format(layout) != "2026-10-16 09:00" {
		t.Fatalf("due %v rule %+v err %v", due, rule, err)
	}
}

// A repeat word still gives the recurring rule, with the same first due.
func TestMonthWeekdayRecurringUnchanged(t *testing.T) {
	got, _ := Parser{}.Parse("x every last friday of the month 4pm", parseNow(t, "", ""), greg)
	want := nthDay(greg, recur.Last, time.Friday, 16, 0)
	if got.Rule == nil || got.Rule.Nth != want.Nth || got.Due.Format(layout) != "2026-10-30 16:00" || got.Text != "x" {
		t.Errorf("got %+v", got)
	}
}

// A repeat word before the phrase keeps the old recurring reading, even when
// the tail after it is not part of the rule ("ماه بعد" stays in the text).
func TestMonthWeekdayRepeatWordWins(t *testing.T) {
	for _, in := range []string{"every last friday ماه بعد", "every last friday ماه بعد @wife"} {
		got, _ := Parser{}.Parse(in, parseNow(t, "", ""), greg)
		if got.Rule == nil || got.Rule.Freq != recur.MonthlyWeekday {
			t.Errorf("%q: got %+v", in, got)
		}
	}
}

// Other repeat heads before the phrase never give a one-off date: the result
// is what it was before (a rule, or plain text).
func TestMonthWeekdayOtherRepeatHeads(t *testing.T) {
	for _, in := range []string{
		"x every 2 months last friday of the month",
		"x every other last friday of the month",
		"x every week last friday of the month",
		"x weekly last friday of the month",
		"x daily last friday of the month",
		"x yearly last friday of the month",
		"x every day last friday of the month",
		"x every friday last friday of the month",
		"x هر هفته آخرین جمعه ماه",
		"x هر دو ماه آخرین جمعه ماه",
		"x هفتگی آخرین جمعه ماه",
	} {
		got, _ := Parser{}.Parse(in, parseNow(t, "", ""), greg)
		if got.Rule == nil && got.Due != nil && got.Due.Format(layout) == "2026-10-30 09:00" {
			t.Errorf("%q: one-off date", in)
		}
	}
}

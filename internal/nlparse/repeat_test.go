package nlparse

import (
	"testing"
	"time"
)

// Default now: Saturday 2026-10-10 08:15 (18 Mehr 1405); tomorrow is Sunday.
var dateRepeatRows = []row{
	// ---- issue #70 table ----
	{in: "call mom tomorrow 15:00 every week", wantText: "call mom", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	{in: "pay rent 1 Nov every month", wantText: "pay rent", wantDue: "2026-11-01 09:00", wantRule: monthly(greg, 1, 9, 0)},
	{in: "اجاره ۱ آبان هر ماه", cal: jal, wantText: "اجاره", wantDue: "2026-10-23 09:00", wantRule: monthly(jal, 1, 9, 0)},
	{in: "گزارش فردا ساعت ۱۵ هر هفته", wantText: "گزارش", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	{in: "گزارش فردا ساعت ۱۵ هر هفته", cal: jal, wantText: "گزارش", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	{in: "every week tomorrow 15:00 call mom", wantText: "call mom", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	{in: "call mom tomorrow 15:00 every week", cal: jal, wantText: "call mom", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	// ---- more shapes ----
	{in: "x tomorrow every week", wantText: "x", wantDue: "2026-10-11 09:00", wantRule: weekly(9, 0, time.Sunday)},
	{in: "x tomorrow at 15:00 every week", wantText: "x", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	{in: "x 15:00 tomorrow every week", wantText: "x", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	{in: "x 15 Oct 8pm every week", wantText: "x", wantDue: "2026-10-15 20:00", wantTime: true, wantRule: weekly(20, 0, time.Thursday)},
	{in: "x 15 Oct every month", wantText: "x", wantDue: "2026-10-15 09:00", wantRule: monthly(greg, 15, 9, 0)},
	{in: "x 15 Oct every month", cal: jal, wantText: "x", wantDue: "2026-10-15 09:00", wantRule: monthly(jal, 23, 9, 0)}, // 15 Oct is 23 Mehr
	{in: "اجاره ۱ آبان هر ماه", cal: greg, wantText: "اجاره", wantDue: "2026-10-23 09:00", wantRule: monthly(greg, 23, 9, 0)},
	{in: "اجاره ۱ آبان ساعت ۱۰ هر ماه", cal: jal, wantText: "اجاره", wantDue: "2026-10-23 10:00", wantTime: true, wantRule: monthly(jal, 1, 10, 0)},
	{in: "pay rent every month 1 Nov", wantText: "pay rent", wantDue: "2026-11-01 09:00", wantRule: monthly(greg, 1, 9, 0)},
	{in: "every month 1 Nov pay rent", wantText: "pay rent", wantDue: "2026-11-01 09:00", wantRule: monthly(greg, 1, 9, 0)},
	{in: "weekly tomorrow 15:00 call mom", wantText: "call mom", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	{in: "call mom tomorrow 15:00 weekly", wantText: "call mom", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	{in: "call mom tomorrow 15:00 monthly", wantText: "call mom", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: monthly(greg, 11, 15, 0)},
	{in: "report 15:00 every week", wantText: "report", wantDue: "2026-10-10 15:00", wantTime: true, wantRule: weekly(15, 0, time.Saturday)},
	{in: "x tomorrow 15:00 every week @wife", wantText: "x @wife", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	// ---- conflicts: the explicit "every <weekday>" wins; the first occurrence is on/after the date ----
	{in: "x next monday every friday", wantText: "x", wantDue: "2026-10-16 09:00", wantRule: weekly(9, 0, time.Friday)},
	{in: "every friday next monday x", wantText: "x", wantDue: "2026-10-16 09:00", wantRule: weekly(9, 0, time.Friday)},
	{in: "x tomorrow 15:00 every monday and friday", wantText: "x", wantDue: "2026-10-12 15:00", wantTime: true, wantRule: weekly(15, 0, time.Monday, time.Friday)},
	{in: "x sunday 15:00 every sunday", wantText: "x", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: weekly(15, 0, time.Sunday)},
	{in: "x 5 Nov every month on the 10th", wantText: "x", wantDue: "2026-11-10 09:00", wantRule: monthly(greg, 10, 9, 0)},
	{in: "x 12 Nov every month on the 10th", wantText: "x", wantDue: "2026-12-10 09:00", wantRule: monthly(greg, 10, 9, 0)},
	// ---- existing behaviour is unchanged ----
	{in: "pills 9am every day", wantText: "pills 9am", wantDue: "2026-10-10 09:00", wantRule: daily(9, 0)},
	{in: "x tomorrow every day", wantText: "x tomorrow", wantDue: "2026-10-10 09:00", wantRule: daily(9, 0)},
}

func TestDateWithRepeat(t *testing.T) { runRows(t, dateRepeatRows) }

func TestParseWhenDateWithRepeat(t *testing.T) {
	now := parseNow(t, "", "")
	due, rule, err := Parser{}.ParseWhen("tomorrow 15:00 every week", now, greg)
	if err != nil {
		t.Fatal(err)
	}
	if got := due.Format(layout); got != "2026-10-11 15:00" {
		t.Errorf("due = %s", got)
	}
	if rule == nil || rule.Hour != 15 || len(rule.Weekdays) != 1 || rule.Weekdays[0] != time.Sunday {
		t.Errorf("rule = %+v", rule)
	}
}

// Yearly needs recur.Yearly and Rule.Month from #69. When it lands: match
// "year"/"سال" in matchRecurrence, add a recur.Yearly case to buildDatedRule
// and firstOccurrence, then replace the skip with real rows such as
// "تولد مامان ۱۵ مهر ساعت ۱۰ هر سال" (Jalali, yearly on 07-15 at 10:00).
func TestDateWithYearlyRepeat(t *testing.T) {
	t.Skip("needs recur yearly from #69")
}

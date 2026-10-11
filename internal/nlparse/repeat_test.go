package nlparse

import (
	"testing"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
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
	{in: "x 15 Oct every month", cal: jal, wantText: "x", wantDue: "2026-10-15 09:00", wantRule: monthly(greg, 15, 9, 0)},   // a Gregorian month name anchors in Gregorian, whatever cal is
	{in: "اجاره ۱ آبان هر ماه", cal: greg, wantText: "اجاره", wantDue: "2026-10-23 09:00", wantRule: monthly(jal, 1, 9, 0)}, // a Jalali month name anchors in Jalali, whatever cal is
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
	// ---- Due is never in the past: the first occurrence of the rule at or after now ----
	{in: "x today 9am every week", now: "2026-10-10 10:00", wantText: "x", wantDue: "2026-10-17 09:00", wantTime: true, wantRule: weekly(9, 0, time.Saturday)},
	{in: "x 11 Oct 9:00 every week", now: "2026-10-11 10:00", wantText: "x", wantDue: "2026-10-18 09:00", wantTime: true, wantRule: weekly(9, 0, time.Sunday)},
	{in: "x امروز ساعت ۸ هر هفته", now: "2026-10-10 10:00", wantText: "x", wantDue: "2026-10-17 08:00", wantTime: true, wantRule: weekly(8, 0, time.Saturday)},
	{in: "x 5 Oct 2026 every month", wantText: "x", wantDue: "2026-11-05 09:00", wantRule: monthly(greg, 5, 9, 0)},
	{in: "x 5 Oct 2026 every month", cal: jal, wantText: "x", wantDue: "2026-11-05 09:00", wantRule: monthly(greg, 5, 9, 0)},
	// ---- a day with no time uses the default time, so Due is an occurrence of the rule ----
	{in: "x today every week", wantText: "x", wantDue: "2026-10-10 09:00", wantRule: weekly(9, 0, time.Saturday)},
	{in: "x today every week", now: "2026-10-10 10:00", wantText: "x", wantDue: "2026-10-17 09:00", wantRule: weekly(9, 0, time.Saturday)},
	{in: "x 11 Oct every week", now: "2026-10-11 10:00", wantText: "x", wantDue: "2026-10-18 09:00", wantRule: weekly(9, 0, time.Sunday)},
	// ---- a date without a year anchors on this year's occurrence ----
	{in: "x 10 Oct every month", now: "2026-10-11 08:15", wantText: "x", wantDue: "2026-11-10 09:00", wantRule: monthly(greg, 10, 9, 0)},
	{in: "x 10 Oct every week", now: "2026-10-11 08:15", wantText: "x", wantDue: "2026-10-17 09:00", wantRule: weekly(9, 0, time.Saturday)},
	// ---- Jalali calendar and month ends ----
	{in: "x 15 Mehr every month", cal: jal, wantText: "x", wantDue: "2026-11-06 09:00", wantRule: monthly(jal, 15, 9, 0)},
	{in: "x 15/7 every month", cal: jal, wantText: "x", wantDue: "2026-11-06 09:00", wantRule: monthly(jal, 15, 9, 0)},
	{in: "x 1 Aban every week", cal: jal, wantText: "x", wantDue: "2026-10-23 09:00", wantRule: weekly(9, 0, time.Friday)},
	{in: "x 31 Farvardin every month", cal: jal, wantText: "x", wantDue: "2026-10-22 09:00", wantRule: monthly(jal, 31, 9, 0)}, // Mehr has 30 days
	{in: "x 31 Jan every month", now: "2026-02-10 08:15", wantText: "x", wantDue: "2026-02-28 09:00", wantRule: monthly(greg, 31, 9, 0)},
	{in: "x 31 Jan every month", wantText: "x", wantDue: "2026-10-31 09:00", wantRule: monthly(greg, 31, 9, 0)},
	// ---- existing behaviour is unchanged ----
	{in: "pills 9am every day", wantText: "pills 9am", wantDue: "2026-10-10 09:00", wantRule: daily(9, 0)},
	{in: "x tomorrow every day", wantText: "x tomorrow", wantDue: "2026-10-10 09:00", wantRule: daily(9, 0)},
}

func TestDateWithRepeat(t *testing.T) { runRows(t, dateRepeatRows) }

// Every combined row yields a Due that is not in the past and is an
// occurrence of its own rule.
func TestDateWithRepeatDueIsAnOccurrence(t *testing.T) { checkDueRows(t, dateRepeatRows) }

func checkDueRows(t *testing.T, rows []row) {
	t.Helper()
	for _, r := range rows {
		cal := r.cal
		if cal == "" {
			cal = greg
		}
		t.Run(r.in+"|"+string(cal), func(t *testing.T) {
			now := parseNow(t, r.now, r.tz)
			got, err := Parser{}.Parse(r.in, now, cal)
			if err != nil || got.Due == nil || got.Rule == nil {
				t.Fatalf("got %+v, err %v", got, err)
			}
			if got.Due.Before(now) {
				t.Errorf("Due %s is before now %s", got.Due.Format(layout), now.Format(layout))
			}
			if next := got.Rule.Next(got.Due.Add(-time.Second), now.Location()); !next.Equal(*got.Due) {
				t.Errorf("Rule.Next(Due-1s) = %s, want Due %s", next.Format(layout), got.Due.Format(layout))
			}
		})
	}
}

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

func yearly(cal calendar.Kind, month, day, h, m int) *recur.Rule {
	return &recur.Rule{Freq: recur.Yearly, Cal: cal, Month: month, MonthDay: day, Hour: h, Minute: m}
}

// Default now: Saturday 2026-10-10 08:15 (18 Mehr 1405).
var yearlyRows = []row{
	// ---- issue #70 table ----
	{in: "تولد مامان ۱۵ مهر ساعت ۱۰ هر سال", wantText: "تولد مامان", wantDue: "2027-10-07 10:00", wantTime: true, wantRule: yearly(jal, 7, 15, 10, 0)},
	{in: "تولد مامان ۱۵ مهر ساعت ۱۰ هر سال", cal: jal, wantText: "تولد مامان", wantDue: "2027-10-07 10:00", wantTime: true, wantRule: yearly(jal, 7, 15, 10, 0)},
	{in: "birthday 15 Oct 10am every year", wantText: "birthday", wantDue: "2026-10-15 10:00", wantTime: true, wantRule: yearly(greg, 10, 15, 10, 0)},
	{in: "birthday 15 Oct 10am every year", cal: jal, wantText: "birthday", wantDue: "2026-10-15 10:00", wantTime: true, wantRule: yearly(greg, 10, 15, 10, 0)},
	// ---- phrasings ----
	{in: "birthday 15 Oct 9am yearly", wantText: "birthday", wantDue: "2026-10-15 09:00", wantTime: true, wantRule: yearly(greg, 10, 15, 9, 0)},
	{in: "every year 15 Oct birthday", wantText: "birthday", wantDue: "2026-10-15 09:00", wantRule: yearly(greg, 10, 15, 9, 0)},
	{in: "yearly 15 Oct 10:00 birthday", wantText: "birthday", wantDue: "2026-10-15 10:00", wantTime: true, wantRule: yearly(greg, 10, 15, 10, 0)},
	{in: "تولد ۱ آبان سالانه", wantText: "تولد", wantDue: "2026-10-23 09:00", wantRule: yearly(jal, 8, 1, 9, 0)},
	{in: "تولد ۱ آبان سالانه", cal: jal, wantText: "تولد", wantDue: "2026-10-23 09:00", wantRule: yearly(jal, 8, 1, 9, 0)},
	{in: "هر سال ۱ آبان ۸ صبح تولد", wantText: "تولد", wantDue: "2026-10-23 08:00", wantTime: true, wantRule: yearly(jal, 8, 1, 8, 0)},
	{in: "x 15 Oct every year @wife", wantText: "x @wife", wantDue: "2026-10-15 09:00", wantRule: yearly(greg, 10, 15, 9, 0)},
	{in: "x tomorrow 15:00 every year", wantText: "x", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: yearly(greg, 10, 11, 15, 0)},
	{in: "x tomorrow 15:00 every year", cal: jal, wantText: "x", wantDue: "2026-10-11 15:00", wantTime: true, wantRule: yearly(jal, 7, 19, 15, 0)},
	// ---- a bare repeat starts today ----
	{in: "x every year", wantText: "x", wantDue: "2026-10-10 09:00", wantRule: yearly(greg, 10, 10, 9, 0)},
	{in: "x every year", cal: jal, wantText: "x", wantDue: "2026-10-10 09:00", wantRule: yearly(jal, 7, 18, 9, 0)},
	{in: "x هر سال", cal: jal, wantText: "x", wantDue: "2026-10-10 09:00", wantRule: yearly(jal, 7, 18, 9, 0)},
	{in: "x every year at 15:00", wantText: "x", wantDue: "2026-10-10 15:00", wantTime: true, wantRule: yearly(greg, 10, 10, 15, 0)},
	{in: "x every year", now: "2026-10-10 10:00", wantText: "x", wantDue: "2027-10-10 09:00", wantRule: yearly(greg, 10, 10, 9, 0)},
	// ---- Due is the first occurrence at or after now ----
	{in: "x 5 Mar every year", wantText: "x", wantDue: "2027-03-05 09:00", wantRule: yearly(greg, 3, 5, 9, 0)},
	{in: "x 5 Oct 2026 every year", wantText: "x", wantDue: "2027-10-05 09:00", wantRule: yearly(greg, 10, 5, 9, 0)},
	{in: "x 10 Oct 9am every year", now: "2026-10-10 10:00", wantText: "x", wantDue: "2027-10-10 09:00", wantTime: true, wantRule: yearly(greg, 10, 10, 9, 0)},
	{in: "x 25 Dec 2030 every year", wantText: "x", wantDue: "2030-12-25 09:00", wantRule: yearly(greg, 12, 25, 9, 0)},
	// ---- 29 Feb and 30 Esfand: start on the next real one; the rule clamps in common years ----
	{in: "x 29 Feb every year", wantText: "x", wantDue: "2028-02-29 09:00", wantRule: yearly(greg, 2, 29, 9, 0)},
	{in: "x 29 Feb every year", now: "2028-03-01 08:15", wantText: "x", wantDue: "2029-02-28 09:00", wantRule: yearly(greg, 2, 29, 9, 0)},
	{in: "x 30 Esfand every year", cal: jal, wantText: "x", wantDue: "2030-03-20 09:00", wantRule: yearly(jal, 12, 30, 9, 0)},
	{in: "x 29 Esfand every year", cal: jal, wantText: "x", wantDue: "2027-03-20 09:00", wantRule: yearly(jal, 12, 29, 9, 0)},
	// ---- not combined ----
	{in: "x tomorrow every day", wantText: "x tomorrow", wantDue: "2026-10-10 09:00", wantRule: daily(9, 0)},
}

func TestDateWithYearlyRepeat(t *testing.T) { runRows(t, yearlyRows) }

// The yearly rows also meet the Due property above.
func TestYearlyRowsDueIsAnOccurrence(t *testing.T) { checkDueRows(t, yearlyRows) }

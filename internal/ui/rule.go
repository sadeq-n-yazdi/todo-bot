package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/sadeq-n-yazdi/todo-bot/internal/calendar"
	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
	"github.com/sadeq-n-yazdi/todo-bot/internal/recur"
)

// DescribeRule renders a stored recurrence rule as a short phrase, such as
// "every Mon, Wed" or "every year on 27 Farvardin". Month names, and the
// "Jalali" word, follow the calendar of the rule itself, not the calendar of
// lc; lc gives the language and digits only. The time of day is left out. A
// rule that does not parse gives "".
func DescribeRule(rule string, lc i18n.Locale) string {
	return describeRule(rule, lc)
}

// ruleMax is the longest phrase in runes that a list row shows. A longer one
// is cut with "…", so a list page stays under the 4096-unit message limit.
// The compact "Assigned to me" rows show no phrase, for the same reason.
const ruleMax = 40

func describeRule(rule string, lc i18n.Locale) string {
	r, err := recur.Parse(rule)
	if err != nil {
		return ""
	}
	rl := i18n.Locale{Lang: lc.Lang, Cal: r.Cal}
	switch r.Freq {
	case recur.Daily:
		return lc.T("ui.rule.daily")
	case recur.Weekly:
		return describeWeekly(r.Weekdays, lc)
	case recur.Monthly:
		key := "ui.rule.monthly"
		if r.Cal == calendar.Jalali {
			key = "ui.rule.monthly.jalali"
		}
		return lc.T(key, ruleDay(lc, r.MonthDay))
	case recur.MonthlyWeekday:
		if len(r.Weekdays) != 1 {
			return ""
		}
		wd := lc.WeekdayName(r.Weekdays[0], false)
		if r.Nth == recur.Last {
			return lc.T("ui.rule.monthly.last", wd)
		}
		return lc.T("ui.rule.monthly.nth."+strconv.Itoa(r.Nth), wd)
	case recur.Yearly:
		if r.Month < 1 || r.Month > 12 {
			return ""
		}
		return lc.T("ui.rule.yearly", lc.Num(r.MonthDay), rl.MonthName(r.Month))
	case recur.EveryN:
		if r.Interval == 1 {
			return lc.T("ui.rule.daily")
		}
		return lc.T("ui.rule.everyn", lc.Num(r.Interval))
	}
	return ""
}

func describeWeekly(days []time.Weekday, lc i18n.Locale) string {
	switch {
	case len(days) == 7:
		return lc.T("ui.rule.daily")
	case isWorkweek(days):
		return lc.T("ui.rule.weekdays")
	}
	if lc.Lang == i18n.Fa {
		days = saturdayFirst(days)
	}
	names := make([]string, len(days))
	for i, d := range days {
		names[i] = lc.WeekdayName(d, true)
	}
	return lc.T("ui.rule.weekly", strings.Join(names, lc.T("ui.rule.sep")))
}

// saturdayFirst orders days Saturday, Sunday, ..., Friday (the Iranian week).
func saturdayFirst(days []time.Weekday) []time.Weekday {
	out := make([]time.Weekday, 0, len(days))
	for _, d := range days {
		if d == time.Saturday {
			out = append(out, d)
		}
	}
	for _, d := range days {
		if d != time.Saturday {
			out = append(out, d)
		}
	}
	return out
}

// isWorkweek reports whether days is exactly Monday to Friday.
func isWorkweek(days []time.Weekday) bool {
	if len(days) != 5 {
		return false
	}
	for i, d := range days {
		if d != time.Weekday(i+1) {
			return false
		}
	}
	return true
}

// ruleDay is a day of the month: "5th" in English, "۵" (Persian digits) in Farsi.
func ruleDay(lc i18n.Locale, n int) string {
	if lc.Lang != i18n.En {
		return lc.Num(n)
	}
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

// ruleSuffix is " <phrase>" for a rule, or "" when the rule has no phrase.
func ruleSuffix(rule string, lc i18n.Locale) string {
	if d := clip(DescribeRule(rule, lc), ruleMax); d != "" {
		return " " + Escape(d)
	}
	return ""
}

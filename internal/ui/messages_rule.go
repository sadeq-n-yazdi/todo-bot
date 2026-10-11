package ui

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

func init() {
	i18n.Register(i18n.En, map[string]string{
		"ui.rule.daily":          "every day",
		"ui.rule.weekdays":       "every weekday",
		"ui.rule.weekly":         "every %s",
		"ui.rule.sep":            ", ",
		"ui.rule.monthly":        "every month on the %s",
		"ui.rule.monthly.jalali": "every Jalali month on the %s",
		"ui.rule.monthly.nth.1":  "every month, 1st %s",
		"ui.rule.monthly.nth.2":  "every month, 2nd %s",
		"ui.rule.monthly.nth.3":  "every month, 3rd %s",
		"ui.rule.monthly.nth.4":  "every month, 4th %s",
		"ui.rule.monthly.last":   "every month, last %s",
		"ui.rule.yearly":         "every year on %s %s",
		"ui.rule.everyn":         "every %s days",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"ui.rule.daily":          "هر روز",
		"ui.rule.weekdays":       "هر روز کاری",
		"ui.rule.weekly":         "هر %s",
		"ui.rule.sep":            "، ",
		"ui.rule.monthly":        "هر ماه روز %s",
		"ui.rule.monthly.jalali": "هر ماه روز %s",
		"ui.rule.monthly.nth.1":  "هر ماه، اولین %s",
		"ui.rule.monthly.nth.2":  "هر ماه، دومین %s",
		"ui.rule.monthly.nth.3":  "هر ماه، سومین %s",
		"ui.rule.monthly.nth.4":  "هر ماه، چهارمین %s",
		"ui.rule.monthly.last":   "هر ماه، آخرین %s",
		"ui.rule.yearly":         "هر سال %s %s",
		"ui.rule.everyn":         "هر %s روز",
	})
}

package ui

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

func init() {
	i18n.Register(i18n.En, map[string]string{
		"ui.list.title":       "📋 Your todos",
		"ui.list.title.paged": "📋 Your todos (%s/%s)",
		"ui.list.empty":       "Nothing to do 🎉",
		"ui.when.today":       "today",
		"ui.when.tomorrow":    "tomorrow",
		"ui.btn.done":         "✅ Done",
		"ui.btn.snooze10m":    "10m",
		"ui.btn.snooze1h":     "1h",
		"ui.btn.tomorrow":     "Tomorrow",
		"ui.reminder.late":    "late — was due %s",
		"ui.digest.overdue":   "⚠️ Overdue",
		"ui.digest.today":     "📅 Today",
		"ui.digest.nodate":    "+%s without date",
		"ui.digest.empty":     "Nothing due today 🎉",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"ui.list.title":       "📋 کارهای شما",
		"ui.list.title.paged": "📋 کارهای شما (%s/%s)",
		"ui.list.empty":       "کاری نمانده 🎉",
		"ui.when.today":       "امروز",
		"ui.when.tomorrow":    "فردا",
		"ui.btn.done":         "✅ انجام شد",
		"ui.btn.snooze10m":    "۱۰ دقیقه",
		"ui.btn.snooze1h":     "۱ ساعت",
		"ui.btn.tomorrow":     "فردا",
		"ui.reminder.late":    "با تأخیر — موعد: %s",
		"ui.digest.overdue":   "⚠️ عقب\u200cافتاده",
		"ui.digest.today":     "📅 امروز",
		"ui.digest.nodate":    "+%s بدون تاریخ",
		"ui.digest.empty":     "امروز کاری نداری 🎉",
	})
}

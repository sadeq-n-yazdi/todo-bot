package web

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// The strings of the edit form (web.edit.*) and of the message that the bot
// sends to a new assignee (web.tg.assigned, docs/WEB-DESIGN.md §5.4 and #98
// §4). GET /api/i18n sends the web.* strings to the app as raw templates, so
// they use only %s and %d. web.tg.assigned is Telegram HTML: the server
// escapes the item text and the name of the editor.
func init() {
	i18n.Register(i18n.En, map[string]string{
		"web.edit.title":     "Edit item %s",
		"web.edit.open":      "Edit",
		"web.edit.text":      "What to do",
		"web.edit.when":      "When",
		"web.edit.when_hint": "tomorrow 18:00, every monday 9:00",
		"web.edit.no_date":   "No date",
		"web.edit.repeat":    "Repeats: %s",
		"web.edit.assignee":  "Assign to",
		"web.edit.save":      "Save changes",
		"web.edit.saved":     "Saved.",
		"web.edit.unchanged": "No changes.",
		"web.edit.gone":      "This item was completed or deleted. Nothing changed.",
		// %s is the @username of the new assignee (plain text).
		"web.edit.nodm": "%s has not started the bot, so they did not get a message.",

		// The first %s is a mention of the editor, the second the item.
		"web.tg.assigned": "%s assigned you an item: %s",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"web.edit.title":     "ویرایش کار %s",
		"web.edit.open":      "ویرایش",
		"web.edit.text":      "چه کاری",
		"web.edit.when":      "چه زمانی",
		"web.edit.when_hint": "فردا ۱۸:۰۰، هر دوشنبه ۹:۰۰",
		"web.edit.no_date":   "بدون تاریخ",
		"web.edit.repeat":    "تکرار: %s",
		"web.edit.assignee":  "محول به",
		"web.edit.save":      "ذخیره\u200cی تغییرات",
		"web.edit.saved":     "ذخیره شد.",
		"web.edit.unchanged": "تغییری داده نشد.",
		"web.edit.gone":      "این کار انجام یا حذف شده است. چیزی تغییر نکرد.",
		"web.edit.nodm":      "%s هنوز ربات را شروع نکرده است، پس پیامی دریافت نکرد.",

		"web.tg.assigned": "%s یک کار را به شما محول کرد: %s",
	})
}

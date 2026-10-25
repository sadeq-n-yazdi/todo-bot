package web

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// The strings of the write API (web.write.*) and of the "did" notice that
// the bot sends to the creating chat (web.tg.did, docs/WEB-DESIGN.md §5.4).
// GET /api/i18n sends them to the app as raw templates, so they use only %s
// and %d. web.tg.did is Telegram HTML: the server escapes the item text.
func init() {
	i18n.Register(i18n.En, map[string]string{
		"web.write.added":    "Added.",
		"web.write.done":     "Done.",
		"web.write.next":     "Next time: %s",
		"web.write.deleted":  "Deleted.",
		"web.write.reminded": "I will remind you %s.",
		"web.write.saved":    "Saved.",

		// The first %s is a mention of the assignee, the second the item text.
		"web.tg.did": "✅ %s did: %s",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"web.write.added":    "اضافه شد.",
		"web.write.done":     "انجام شد.",
		"web.write.next":     "نوبت بعد: %s",
		"web.write.deleted":  "حذف شد.",
		"web.write.reminded": "%s یادآوری می\u200cکنم.",
		"web.write.saved":    "ذخیره شد.",

		"web.tg.did": "✅ %s انجام داد: %s",
	})
}

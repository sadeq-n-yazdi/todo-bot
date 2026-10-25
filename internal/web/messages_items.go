package web

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// The strings of the item views (web.item.*). GET /api/i18n sends them to
// the app as raw templates, so they use only %s and %d.
func init() {
	i18n.Register(i18n.En, map[string]string{
		// Shown when an assignee has no known @username.
		"web.item.noname":  "a user without a username",
		"web.item.overdue": "Overdue",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"web.item.noname":  "کاربری بدون نام کاربری",
		"web.item.overdue": "گذشته از موعد",
	})
}

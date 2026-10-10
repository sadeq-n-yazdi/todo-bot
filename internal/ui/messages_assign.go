package ui

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// Strings for the "Assigned to me" section of /list (#58).
func init() {
	i18n.Register(i18n.En, map[string]string{
		"ui.assign.title": "Assigned to me",
		"ui.assign.more":  "…and %s more",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"ui.assign.title": "سپرده\u200cشده به من",
		"ui.assign.more":  "…و %s مورد دیگر",
	})
}

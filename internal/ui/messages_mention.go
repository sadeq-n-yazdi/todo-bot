package ui

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// Strings for Mention (#58).
func init() {
	i18n.Register(i18n.En, map[string]string{
		"ui.mention.noname": "member",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"ui.mention.noname": "عضو",
	})
}

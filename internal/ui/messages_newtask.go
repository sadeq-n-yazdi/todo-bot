package ui

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// The "➕ New task" label (#68): the reply-keyboard button in private chats
// and the inline button under /list. The handler recognises it in both
// languages (IsNewTaskLabel), so change it only together with that.
func init() {
	i18n.Register(i18n.En, map[string]string{
		"ui.newtask.label": "➕ New task",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"ui.newtask.label": "➕ کار جدید",
	})
}

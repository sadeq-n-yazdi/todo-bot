package ui

import (
	"strconv"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

// Mention renders a link to the user for HTML parse mode, used for the
// assignee of an item (#58). It links by user ID (tg://user?id=), so it
// pings the right account even if the @username has moved to someone who
// never talked to the bot. The text is "@username" (stored without "@"), or
// a translated label when username is "". userID 0 renders "".
func Mention(userID int64, username string, lc i18n.Locale) string {
	if userID == 0 {
		return ""
	}
	label := lc.T("ui.mention.noname")
	if username != "" {
		label = "@" + Escape(username)
	}
	return `<a href="tg://user?id=` + strconv.FormatInt(userID, 10) + `">` + label + "</a>"
}

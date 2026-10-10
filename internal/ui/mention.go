package ui

// Mention renders a Telegram @username (stored without "@") for HTML parse
// mode, or "" when username is "". Used for the assignee of an item (#58).
func Mention(username string) string {
	if username == "" {
		return ""
	}
	return "@" + Escape(username)
}

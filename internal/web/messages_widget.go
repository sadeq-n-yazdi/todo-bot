package web

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// The strings of the Telegram Login Widget on the login screen (web.widget.*,
// docs/WEB-DESIGN.md §3.4). web.widget.error_<code> is the text for each
// code of /#login-error=<code> (auth_widget.go). They go to the app as raw
// templates and use no verbs.
func init() {
	i18n.Register(i18n.En, map[string]string{
		"web.widget.hint":                "On a computer, you can also sign in with your Telegram account:",
		"web.widget.error_bad_request":   "Telegram sent sign-in data that is not correct. Try again.",
		"web.widget.error_bad_signature": "Telegram did not confirm this sign-in. Try again.",
		"web.widget.error_expired":       "The sign-in took too long. Try again.",
		"web.widget.error_not_allowed":   "This Telegram account cannot use this app. Ask the owner to add it.",
		"web.widget.error_used":          "This sign-in was already used. Sign in again.",
		"web.widget.error_internal":      "Something went wrong with the sign-in. Try again.",
		"web.widget.error_restart":       "Start the sign-in with Telegram again from this page.",
		"web.widget.error_other_user":    "This browser is already signed in with a different account. Log out first.",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"web.widget.hint":                "در رایانه می\u200cتوانید با حساب تلگرام خود هم وارد شوید:",
		"web.widget.error_bad_request":   "داده\u200cهای ورودی که تلگرام فرستاد درست نیست. دوباره امتحان کنید.",
		"web.widget.error_bad_signature": "تلگرام این ورود را تأیید نکرد. دوباره امتحان کنید.",
		"web.widget.error_expired":       "ورود بیش از حد طول کشید. دوباره امتحان کنید.",
		"web.widget.error_not_allowed":   "این حساب تلگرام اجازهٔ استفاده از این برنامه را ندارد. از مالک بخواهید آن را اضافه کند.",
		"web.widget.error_used":          "از این ورود قبلاً استفاده شده است. دوباره وارد شوید.",
		"web.widget.error_internal":      "در ورود مشکلی پیش آمد. دوباره امتحان کنید.",
		"web.widget.error_restart":       "ورود با تلگرام را دوباره از همین صفحه شروع کنید.",
		"web.widget.error_other_user":    "این مرورگر با حساب دیگری وارد شده است. اول خارج شوید.",
	})
}

package web

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// The strings of the login code (web.login.*, docs/WEB-DESIGN.md §3.3, §7.3)
// and of the sign-in notice that the bot sends (web.tg.signin*, §3.3.1). The
// web.login.* strings go to the app as raw templates, so they use only %s.
func init() {
	i18n.Register(i18n.En, map[string]string{
		"web.login.title":         "Sign in",
		"web.login.steps":         "1. Open the app in Telegram.\n2. Go to Settings, then Sign in a browser.\n3. Type the code here.",
		"web.login.open":          "Open the app in Telegram",
		"web.login.code_label":    "Code",
		"web.login.submit":        "Sign in",
		"web.login.busy":          "Checking the code...",
		"web.login.code_title":    "Sign in a browser",
		"web.login.code_warning":  "Type this code only into your own browser. Never send it to another person.",
		"web.login.code_left":     "Time left: %s",
		"web.login.code_expired":  "The code expired.",
		"web.login.code_cancel":   "Cancel the code",
		"web.login.code_canceled": "The code is canceled.",
		"web.login.code_new":      "Get a new code",
		"web.login.signout_title": "Log out everywhere",
		"web.login.signout_text":  "This logs out every browser and app that is signed in to your account, this one too.",
		"web.login.signout_ok":    "Log out",
		"web.login.signout_done":  "You are logged out everywhere.",

		"web.tg.signin":        "New sign-in to the web app from %s (%s). If this was not you, log out everywhere.",
		"web.tg.signin_logout": "Log out everywhere",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"web.login.title":         "ورود",
		"web.login.steps":         "۱. برنامه را در تلگرام باز کنید.\n۲. به تنظیمات بروید و «ورود در مرورگر» را بزنید.\n۳. کد را همین\u200cجا بنویسید.",
		"web.login.open":          "باز کردن برنامه در تلگرام",
		"web.login.code_label":    "کد",
		"web.login.submit":        "ورود",
		"web.login.busy":          "در حال بررسی کد...",
		"web.login.code_title":    "ورود در مرورگر",
		"web.login.code_warning":  "این کد را فقط در مرورگر خودتان بنویسید. هرگز آن را برای کس دیگری نفرستید.",
		"web.login.code_left":     "زمان باقی\u200cمانده: %s",
		"web.login.code_expired":  "کد منقضی شد.",
		"web.login.code_cancel":   "لغو کد",
		"web.login.code_canceled": "کد لغو شد.",
		"web.login.code_new":      "گرفتن کد تازه",
		"web.login.signout_title": "خروج از همه\u200cجا",
		"web.login.signout_text":  "با این کار همهٔ مرورگرها و برنامه\u200cهایی که در حساب شما وارد شده\u200cاند، از جمله همین یکی، خارج می\u200cشوند.",
		"web.login.signout_ok":    "خروج",
		"web.login.signout_done":  "از همه\u200cجا خارج شدید.",

		"web.tg.signin":        "ورود تازه به برنامهٔ وب از %s (%s). اگر این شما نبودید، از همه\u200cجا خارج شوید.",
		"web.tg.signin_logout": "خروج از همه\u200cجا",
	})
}

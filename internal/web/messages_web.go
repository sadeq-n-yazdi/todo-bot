package web

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// The strings of the app shell (web.app.*) and of every API error code
// (web.err.<code>, docs/WEB-DESIGN.md §5.1). GET /api/i18n sends them to the
// app as raw templates, so they use only %s and %d.
func init() {
	i18n.Register(i18n.En, map[string]string{
		"web.app.notallowed":       "Your Telegram ID is %s. Ask the owner to add it.",
		"web.app.open_in_telegram": "Open this page from the bot in Telegram.",
		"web.app.error":            "Something went wrong.",

		"web.err.bad_request":           "The request is not correct.",
		"web.err.bad_signature":         "Telegram did not sign this request. Open the app from the bot again.",
		"web.err.expired":               "The sign-in data is too old. Open the app from the bot again.",
		"web.err.not_allowed":           "You are not allowed to use this bot. Ask the owner to add your Telegram ID.",
		"web.err.unauthenticated":       "Sign in again.",
		"web.err.session_revoked":       "Your access was removed. Ask the owner.",
		"web.err.csrf":                  "The request was refused for security reasons.",
		"web.err.too_large":             "The request is too large.",
		"web.err.rate_limited":          "Too many requests. Wait a moment and try again.",
		"web.err.internal":              "Something went wrong on the server. Try again later.",
		"web.err.not_implemented":       "This is not available yet.",
		"web.err.not_found":             "Not found.",
		"web.err.not_member":            "You are not a member of this group.",
		"web.err.group_moved":           "This group moved to a new ID. Open it again.",
		"web.err.membership_unverified": "Telegram cannot confirm that you are a member of this group now. Try again later.",
		"web.err.bad_lang":              "This language is not supported.",
		"web.err.bad_calendar":          "This calendar is not supported.",
		"web.err.wrong_user":            "This Telegram account is not the account of this session.",
		"web.err.too_many_codes":        "Too many sign-in codes are open now. Try again in a few minutes.",
		"web.err.bad_code":              "The code is wrong, used or expired. Get a new code in the app.",
		"web.err.empty_text":            "Write a text for the item.",
		"web.err.too_long":              "The text is too long.",
		"web.err.unknown_assignee":      "This person cannot get items.",
		"web.err.in_past":               "This time is in the past.",
		"web.err.bad_time":              "The time is not correct.",
		"web.err.wrong_session":         "Do this in the app in Telegram.",
		"web.err.misdirected":           "Wrong address.",
		"web.err.busy":                  "The server is busy. Try again in a moment.",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"web.app.notallowed":       "شناسهٔ تلگرام شما %s است. از مالک بخواهید آن را اضافه کند.",
		"web.app.open_in_telegram": "این صفحه را از ربات در تلگرام باز کنید.",
		"web.app.error":            "مشکلی پیش آمد.",

		"web.err.bad_request":           "درخواست درست نیست.",
		"web.err.bad_signature":         "تلگرام این درخواست را امضا نکرده است. برنامه را دوباره از ربات باز کنید.",
		"web.err.expired":               "اطلاعات ورود قدیمی است. برنامه را دوباره از ربات باز کنید.",
		"web.err.not_allowed":           "شما اجازهٔ استفاده از این ربات را ندارید. از مالک بخواهید شناسهٔ تلگرام شما را اضافه کند.",
		"web.err.unauthenticated":       "دوباره وارد شوید.",
		"web.err.session_revoked":       "دسترسی شما برداشته شد. از مالک بپرسید.",
		"web.err.csrf":                  "درخواست به دلایل امنیتی رد شد.",
		"web.err.too_large":             "درخواست خیلی بزرگ است.",
		"web.err.rate_limited":          "درخواست\u200cها خیلی زیاد است. کمی صبر کنید و دوباره امتحان کنید.",
		"web.err.internal":              "مشکلی در سرور پیش آمد. بعداً دوباره امتحان کنید.",
		"web.err.not_implemented":       "این هنوز در دسترس نیست.",
		"web.err.not_found":             "پیدا نشد.",
		"web.err.not_member":            "شما عضو این گروه نیستید.",
		"web.err.group_moved":           "این گروه به شناسهٔ تازه\u200cای منتقل شد. آن را دوباره باز کنید.",
		"web.err.membership_unverified": "تلگرام اکنون نمی\u200cتواند عضویت شما را در این گروه تأیید کند. بعداً دوباره امتحان کنید.",
		"web.err.bad_lang":              "این زبان پشتیبانی نمی\u200cشود.",
		"web.err.bad_calendar":          "این تقویم پشتیبانی نمی\u200cشود.",
		"web.err.wrong_user":            "این حساب تلگرام، حساب این نشست نیست.",
		"web.err.too_many_codes":        "اکنون کدهای ورود باز خیلی زیاد است. چند دقیقهٔ دیگر دوباره امتحان کنید.",
		"web.err.bad_code":              "کد اشتباه، استفاده\u200cشده یا منقضی است. در برنامه کد تازه بگیرید.",
		"web.err.empty_text":            "برای کار متنی بنویسید.",
		"web.err.too_long":              "متن خیلی طولانی است.",
		"web.err.unknown_assignee":      "این شخص نمی\u200cتواند کار بگیرد.",
		"web.err.in_past":               "این زمان گذشته است.",
		"web.err.bad_time":              "زمان درست نیست.",
		"web.err.wrong_session":         "این کار را در برنامه در تلگرام انجام دهید.",
		"web.err.misdirected":           "نشانی اشتباه است.",
		"web.err.busy":                  "سرور شلوغ است. کمی بعد دوباره امتحان کنید.",
	})
}

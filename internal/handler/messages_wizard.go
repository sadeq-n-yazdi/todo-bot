package handler

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// Strings of the step-by-step wizard (#68). The card texts live in ui; these
// are the handler's own toasts, notes and fallbacks.
func init() {
	i18n.Register(i18n.En, map[string]string{
		"wizard.starting":      "Starting…",
		"wizard.cancelled":     "✖ Cancelled.",
		"wizard.toast.expired": "This card is closed. To add a new item, use /new.",
		"wizard.toast.foreign": "Only the person who started this can use it.",
		"wizard.toast.stale":   "This card was out of date — here is the current step.",
		"wizard.toast.past":    "That day has passed.",
		"wizard.note.empty":    "The text can't be empty.",
		"wizard.note.toolong":  "That text is too long (%s characters). The limit is %s.",
		"wizard.note.badtime":  "I could not read that time. Try 15:30 or 3pm.",
		"wizard.note.passed":   "That time has passed. Please choose again.",
		"wizard.note.assignee": "That person can't be assigned items any more. Please choose again.",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"wizard.starting":      "در حال شروع…",
		"wizard.cancelled":     "✖ لغو شد.",
		"wizard.toast.expired": "این کارت بسته شده است. برای افزودن کار تازه از /new استفاده کنید.",
		"wizard.toast.foreign": "فقط کسی که این را شروع کرده می\u200cتواند از آن استفاده کند.",
		"wizard.toast.stale":   "این کارت قدیمی بود — مرحله فعلی نمایش داده شد.",
		"wizard.toast.past":    "این روز گذشته است.",
		"wizard.note.empty":    "متن نمی\u200cتواند خالی باشد.",
		"wizard.note.toolong":  "این متن خیلی طولانی است (%s نویسه). حداکثر مجاز %s نویسه است.",
		"wizard.note.badtime":  "این زمان را نفهمیدم. مثلاً ۱۵:۳۰ یا 3pm بنویسید.",
		"wizard.note.passed":   "این زمان گذشته است. لطفاً دوباره انتخاب کنید.",
		"wizard.note.assignee": "دیگر نمی\u200cتوان کار را به این فرد محول کرد. لطفاً دوباره انتخاب کنید.",
	})
}

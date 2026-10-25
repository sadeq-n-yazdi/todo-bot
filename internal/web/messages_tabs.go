package web

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// The strings of the tabs, the lists and the PWA (web.tabs.*,
// docs/WEB-DESIGN.md §7.3 and §7.5). GET /api/i18n sends them to the app as
// raw templates, so they use only %s and %d.
func init() {
	i18n.Register(i18n.En, map[string]string{
		"web.tabs.label":          "Lists",
		"web.tabs.mine":           "Mine",
		"web.tabs.assigned":       "Assigned to me",
		"web.tabs.groups":         "Groups",
		"web.tabs.chips":          "Groups",
		"web.tabs.empty.mine":     "You have no open items.",
		"web.tabs.empty.assigned": "Nobody gave you an item.",
		"web.tabs.empty.groups":   "You are not in a group with this bot.",
		"web.tabs.empty.group":    "This group has no open items.",
		"web.tabs.more":           "Load more",
		"web.tabs.retry":          "Try again",
		"web.tabs.error":          "The list did not load.",
		"web.tabs.offline":        "No connection. This list is from %s. You cannot change items now.",
		"web.tabs.offline_none":   "No connection, and there is no saved list.",
		"web.tabs.partial":        "Some groups may be missing. Try again later.",
		"web.tabs.noname":         "(no username)",
		"web.tabs.update":         "A new version is ready.",
		"web.tabs.reload":         "Reload",
		"web.tabs.install":        "Install",
		"web.tabs.ios_hint":       "To install the app, tap Share, then Add to Home Screen.",
		"web.tabs.close":          "Close",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"web.tabs.label":          "فهرست\u200cها",
		"web.tabs.mine":           "من",
		"web.tabs.assigned":       "سپرده\u200cشده به من",
		"web.tabs.groups":         "گروه\u200cها",
		"web.tabs.chips":          "گروه\u200cها",
		"web.tabs.empty.mine":     "موردی باز ندارید.",
		"web.tabs.empty.assigned": "کسی موردی به شما نسپرده است.",
		"web.tabs.empty.groups":   "در هیچ گروهی با این ربات نیستید.",
		"web.tabs.empty.group":    "این گروه موردی باز ندارد.",
		"web.tabs.more":           "نمایش بیشتر",
		"web.tabs.retry":          "تلاش دوباره",
		"web.tabs.error":          "فهرست بارگیری نشد.",
		"web.tabs.offline":        "اتصال ندارید. این فهرست مربوط به ساعت %s است. اکنون نمی\u200cتوانید موردها را تغییر دهید.",
		"web.tabs.offline_none":   "اتصال ندارید و فهرست ذخیره\u200cشده\u200cای نیست.",
		"web.tabs.partial":        "ممکن است بعضی گروه\u200cها نمایش داده نشوند. بعداً دوباره تلاش کنید.",
		"web.tabs.noname":         "(بدون نام کاربری)",
		"web.tabs.update":         "نسخهٔ جدید آماده است.",
		"web.tabs.reload":         "بارگذاری دوباره",
		"web.tabs.install":        "نصب",
		"web.tabs.ios_hint":       "برای نصب برنامه روی «اشتراک\u200cگذاری» بزنید، سپس «افزودن به صفحهٔ اصلی».",
		"web.tabs.close":          "بستن",
	})
}

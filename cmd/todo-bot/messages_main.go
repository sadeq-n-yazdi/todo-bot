package main

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

// Descriptions of the bot menu (setMyCommands), one key per command.
func init() {
	i18n.Register(i18n.En, map[string]string{
		"cmd.menu.help":     "Show help",
		"cmd.menu.add":      "Add an item",
		"cmd.menu.list":     "Show open items",
		"cmd.menu.done":     "Mark an item done",
		"cmd.menu.del":      "Delete an item",
		"cmd.menu.edit":     "Change an item's text",
		"cmd.menu.today":    "Overdue and today's items",
		"cmd.menu.remind":   "Add a reminder to an item",
		"cmd.menu.tz":       "Set your time zone",
		"cmd.menu.digest":   "Set the daily digest",
		"cmd.menu.lang":     "Choose the language",
		"cmd.menu.calendar": "Choose the calendar",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"cmd.menu.help":     "نمایش راهنما",
		"cmd.menu.add":      "افزودن کار",
		"cmd.menu.list":     "نمایش کارهای باز",
		"cmd.menu.done":     "انجام\u200cشدن کار",
		"cmd.menu.del":      "حذف کار",
		"cmd.menu.edit":     "ویرایش متن کار",
		"cmd.menu.today":    "کارهای امروز و عقب\u200cافتاده",
		"cmd.menu.remind":   "افزودن یادآور به کار",
		"cmd.menu.tz":       "تنظیم منطقه زمانی",
		"cmd.menu.digest":   "تنظیم خلاصه روزانه",
		"cmd.menu.lang":     "انتخاب زبان",
		"cmd.menu.calendar": "انتخاب تقویم",
	})
}

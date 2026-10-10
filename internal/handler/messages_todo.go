package handler

import "github.com/sadeq-n-yazdi/todo-bot/internal/i18n"

func init() {
	i18n.Register(i18n.En, map[string]string{
		"todo.help": "<b>Todo bot</b>\n" +
			"Send plain text in a private chat to add an item.\n\n" +
			"/add &lt;text&gt; [when] - add an item, e.g. <code>buy milk tomorrow 18:00</code>, " +
			"<code>pills every day 9am</code>\n" +
			"Start the text with @name to assign it to a member: <code>/add @sam call the bank 3pm</code>\n" +
			"/list [page] - show open items\n" +
			"/done &lt;n&gt; - mark item n done\n" +
			"/del &lt;n&gt; - delete item n\n" +
			"/edit &lt;n&gt; &lt;text&gt; - change the text of item n\n" +
			"/today - overdue and today's items\n" +
			"/remind &lt;n&gt; &lt;when&gt; - add a reminder to item n\n" +
			"/tz, /digest, /lang, /calendar - settings\n" +
			"/whoami - show your Telegram ID",
		"todo.usage.add":            "Usage: <code>/add buy milk tomorrow 18:00</code>",
		"todo.usage.list":           "Usage: <code>/list [page]</code>",
		"todo.usage.done":           "Usage: <code>/done 3</code>",
		"todo.usage.del":            "Usage: <code>/del 3</code>",
		"todo.usage.edit":           "Usage: <code>/edit 3 new text</code>",
		"todo.err.toolong":          "That text is too long (%s characters). The limit is %s.",
		"todo.err.notfound":         "There is no open item with that number.",
		"todo.err.foreign":          "That item belongs to another chat.",
		"todo.err.past":             "That time is already in the past.",
		"todo.unknown":              "Unknown command. Send /help for the list of commands.",
		"todo.added":                "✅ Added %s. %s",
		"todo.done":                 "✅ Done: %s. %s",
		"todo.done.next":            "Next: %s",
		"todo.deleted":              "🗑 Deleted: %s. %s",
		"todo.edited":               "✏️ Updated: %s. %s",
		"todo.assign.unknown":       "@%s hasn't started the bot yet — ask them to open @%s and press Start.",
		"todo.assign.did":           "✅ %s did: %s",
		"todo.assign.unknown.nobot": "@%s hasn't started the bot yet — ask them to open the bot and press Start.",
	})
	i18n.Register(i18n.Fa, map[string]string{
		"todo.help": "<b>ربات کارها</b>\n" +
			"در گفت\u200cوگوی خصوصی هر متنی بفرستید تا به\u200cعنوان کار اضافه شود.\n\n" +
			"/add &lt;متن&gt; [زمان] - افزودن کار، مثلاً <code>خرید نان فردا ساعت ۱۸</code>، " +
			"<code>قرص هر روز ساعت ۹</code>\n" +
			"برای محول کردن کار به یک عضو، متن را با @name شروع کنید: <code>/add @sam call the bank 3pm</code>\n" +
			"/list [صفحه] - نمایش کارهای باز\n" +
			"/done &lt;n&gt; - انجام\u200cشدن کار شماره n\n" +
			"/del &lt;n&gt; - حذف کار شماره n\n" +
			"/edit &lt;n&gt; &lt;متن&gt; - تغییر متن کار شماره n\n" +
			"/today - کارهای عقب\u200cافتاده و امروز\n" +
			"/remind &lt;n&gt; &lt;زمان&gt; - افزودن یادآور به کار شماره n\n" +
			"/tz، /digest، /lang، /calendar - تنظیمات\n" +
			"/whoami - نمایش شناسه تلگرام شما",
		"todo.usage.add":            "راهنما: <code>/add خرید نان فردا ساعت 18</code>",
		"todo.usage.list":           "راهنما: <code>/list [page]</code>",
		"todo.usage.done":           "راهنما: <code>/done 3</code>",
		"todo.usage.del":            "راهنما: <code>/del 3</code>",
		"todo.usage.edit":           "راهنما: <code>/edit 3 متن جدید</code>",
		"todo.err.toolong":          "این متن خیلی طولانی است (%s نویسه). حداکثر مجاز %s نویسه است.",
		"todo.err.notfound":         "کار بازی با این شماره وجود ندارد.",
		"todo.err.foreign":          "این کار متعلق به گفت\u200cوگوی دیگری است.",
		"todo.err.past":             "این زمان گذشته است.",
		"todo.unknown":              "دستور ناشناخته است. برای دیدن فهرست دستورها /help را بفرستید.",
		"todo.added":                "✅ اضافه شد: %s. %s",
		"todo.done":                 "✅ انجام شد: %s. %s",
		"todo.done.next":            "نوبت بعدی: %s",
		"todo.deleted":              "🗑 حذف شد: %s. %s",
		"todo.edited":               "✏️ ویرایش شد: %s. %s",
		"todo.assign.unknown":       "@%s هنوز ربات را شروع نکرده است — از او بخواهید @%s را باز کند و Start را بزند.",
		"todo.assign.did":           "✅ %s انجام داد: %s",
		"todo.assign.unknown.nobot": "@%s هنوز ربات را شروع نکرده است — از او بخواهید ربات را باز کند و Start را بزند.",
	})
}

# Usage

This page lists every command, with examples in English and in Farsi. For setup, see the [README](../README.md). A Farsi quick start is in [README.fa.md](../README.fa.md).

Replies below are shown for a user whose time zone is `Asia/Tehran`, at Saturday 10 Oct 2026, 08:15. Words such as "today" and "tomorrow" depend on the time you send the message. Replies in Farsi use Persian digits (۰۱۲۳۴۵۶۷۸۹), shown here as the bot writes them.

## Basics

- **Command names are always Latin** (`/add`, `/list`), as Telegram requires. The text after them can be English or Farsi.
- **Item numbers** belong to a chat. Item 3 in your private chat is not item 3 in a group. Numbers are shown in the list and used by `/done`, `/del`, `/edit` and `/remind`.
- **Digits.** Persian (`۱۲۳`) and Arabic-Indic (`١٢٣`) digits are accepted everywhere, and are turned into ordinary digits before the bot reads them. `/done ۳` and `/done 3` are the same.
- **Languages are mixed freely.** English and Farsi time phrases are understood whatever language and calendar you chose with `/lang` and `/calendar`.
- **Plain text in a private chat adds an item**, exactly like `/add`. In a group, only commands are read.
- **Unknown commands** in a private chat get "Unknown command. Send /help for the list of commands."

## Writing the time

`/add` (and plain text) looks for a date or time at the **end** of what you write. The rest is the item's text. A phrase in the middle, like `monday meeting notes` or `fix 2 bugs`, stays plain text.

| You write | Meaning |
| --- | --- |
| `buy milk tomorrow` | Tomorrow at `TODO_DEFAULT_TIME` (09:00 by default). |
| `buy milk tomorrow 18:00`, `tomorrow at 6:30pm` | Tomorrow at that time. |
| `call mom in 2h`, `in 10m`, `in 45 minutes` | That long from now. |
| `stretch in 3 days at 14:00` | Three days from now at 14:00. |
| `pills 9am` | The next 9:00. If it has passed today, tomorrow. |
| `walk tonight` | Today at 20:00. |
| `gym on friday at 14:30`, `gym next monday` | The weekday. |
| `tax 15 Oct`, `tax Oct 15 2027`, `tax 2026-10-15`, `tax 15/10` | A Gregorian date. |
| `tax 25 Mehr`, `tax 1405/07/25` | A Jalali date. A Jalali month name always means Jalali. |
| `خرید نان فردا ساعت ۱۸` | Tomorrow 18:00. |
| `استراحت ۲ ساعت دیگر` | In two hours. |
| `باشگاه پنج‌شنبه ساعت ۱۷` | Thursday 17:00. |
| `مالیات ۲۵ مهر` | 25 Mehr. |

A date with no year means the next time it comes. A time with no date that has already passed today means tomorrow. A date with no time that is today means the end of today (23:59) if `TODO_DEFAULT_TIME` has already passed.

Numeric dates such as `25/7` follow **your calendar**: day/month in Jalali if you chose Jalali, Gregorian otherwise. `2026-10-15` is always Gregorian.

## Recurring items

Add a recurrence phrase to `/add`. The item stays open. When you mark it done, it moves to its next time.

| English | Farsi | Repeats |
| --- | --- | --- |
| `pills every day 9am` | `قرص هر روز ساعت ۹` | Every day at 9:00. `daily` also works. |
| `report weekdays 9:30` | `گزارش روزهای کاری ساعت ۹:۳۰` | English `weekdays` is Monday to Friday. Farsi `روزهای کاری` is Saturday to Wednesday. |
| `run every monday and wednesday 8:00` | `ورزش هر دوشنبه و چهارشنبه ساعت ۸` | Those weekdays at 8:00. |
| `run every week on tue 8:00` | `ورزش هر هفته سه‌شنبه ساعت ۸` | Weekly on that day. `run every friday` also works. |
| `rent every month on the 1st at 10:00` | `اجاره هر ماه ۱ام ساعت ۱۰` | Monthly at 10:00. The day follows **your calendar**: the 1st of each Jalali month for a Jalali user. A day the month does not have (31st in a 30-day month) falls on the last day. |
| `water plants every 3 days` | `آب دادن هر ۳ روز` | Every 3 days at 9:00 (`TODO_DEFAULT_TIME`). |

A recurring item shows 🔁 in lists.

Recurring phrases work in `/add` and plain text. `/remind` does not accept them.

## Commands

### `/start`, `/help`

Shows the introduction and the list of commands. They are the same command.

### `/whoami`

Replies with your numeric Telegram user ID, for example `123456789`. It works in a private chat, for everybody, even if you are not on the allowlist. Use it to get the number for `TODO_ALLOWED_USERS`.

### `/add <text> [when]`

Adds an item. An item with a future date or time gets a reminder, and an item with no date gets none. A date without a time (`buy milk tomorrow`) is reminded at `TODO_DEFAULT_TIME`. A date-only item for today whose default time has already passed is stored as overdue, without a reminder. A message with no command in a private chat does the same.

```
/add buy milk tomorrow 18:00
/add pills every day 9am
/add rent every month on the 1st at 10:00
```

```
/add خرید نان فردا ساعت ۱۸
/add اجاره هر ماه ۱ام ساعت ۱۰
/add جلسه ۲۵ مهر ساعت ۹
```

Replies:

```
✅ Added 3. buy milk — tomorrow 18:00
✅ Added 4. pills — today 09:00 🔁
```

```
✅ اضافه شد: ۳. خرید نان — فردا ۱۸:۰۰
✅ اضافه شد: ۵. اجاره — جمعه ۱ آبان ۱۰:۰۰ 🔁
```

With no text, the bot answers `Usage: /add buy milk tomorrow 18:00`. The limit is 1000 characters per item.

### `/list [page]`

Shows your open items, ten per page. With a page number, shows that page. A page past the end shows the last page.

```
/list
/list 2
/list ۲
```

Reply:

```
📋 Your todos
3. buy milk — tomorrow 18:00
4. pills — today 09:00 🔁
5. call the bank
```

In Farsi:

```
📋 کارهای شما
۳. خرید نان — فردا ۱۸:۰۰
۴. قرص — امروز ۰۹:۰۰ 🔁
۵. تماس با بانک
```

When there are several pages the title shows the page, for example `📋 Your todos (1/2)`. An item whose time has passed has ⚠️ after it. An empty list says `Nothing to do 🎉` (`کاری نمانده 🎉`).

Under the text are buttons, see [List buttons](#list-buttons).

### `/done <n>`

Marks item n done. A recurring item stays open and moves to its next time.

```
/done 3
/done ۳
/done #3
```

Replies:

```
✅ Done: 3. buy milk
```

```
✅ انجام شد: ۳. خرید نان
```

For a recurring item, a second line is added: `Next: tomorrow 09:00` (`نوبت بعدی: فردا ۰۹:۰۰`). If there is no open item with that number: "There is no open item with that number."

### `/del <n>`

Deletes item n, without asking. (The 🗑 button asks first.)

```
/del 3
/del ۳
```

Reply: `🗑 Deleted: 3. buy milk`, in Farsi `🗑 حذف شد: ۳. خرید نان`.

### `/edit <n> <text>`

Changes the text of item n. The time and reminders stay as they are, and the new text is **not** searched for a date.

```
/edit 3 buy oat milk
/edit ۳ خرید نان سنگک
```

Reply: `✏️ Updated: 3. buy oat milk`, in Farsi `✏️ ویرایش شد: ۳. خرید نان سنگک`.

### `/today`

Shows the digest on demand: overdue items and items due today, plus how many items have no date.

```
⚠️ Overdue
2. send the report — Fri 9 Oct 17:00 ⚠️

📅 Today
4. pills — today 09:00 🔁

+3 without date
```

In Farsi the headings are `⚠️ عقب‌افتاده` and `📅 امروز`, and the last line is `+۳ بدون تاریخ`. If nothing is due: `Nothing due today 🎉` (`امروز کاری ندارید 🎉`).

### `/remind <n> <when>` and `/remind <text> <when>`

Adds a reminder to an existing item, or creates an item that has one.

```
/remind 3 tomorrow 18:00
/remind pay rent friday 9am
/remind ۳ فردا ساعت ۱۸
/remind پرداخت اجاره جمعه ساعت ۹
```

Replies:

```
⏰ Reminder set for tomorrow 18:00.
3. buy milk
```

```
✅ Added 6. pay rent — Fri 16 Oct 09:00
```

Farsi: `⏰ یادآور برای فردا ۱۸:۰۰ تنظیم شد.` and `✅ افزوده شد ۶. پرداخت اجاره — جمعه ۱۶ اکتبر ۰۹:۰۰` (or `... ۲۴ مهر ...` for a Jalali user).

Notes:
- `/remind 2 pizzas tomorrow` makes an item called "2 pizzas", because `pizzas` is not a time. A number is taken as an item number only when what follows it is a time.
- Recurring phrases are refused: "Recurring reminders aren't supported here. Use /add for repeating items."
- With no time, the bot says "I couldn't find a time. Try “tomorrow 18:00” or “in 2h”."
- Without any text, it shows the usage.

### `/tz [Area/City]`

Shows or sets **your** time zone, using an IANA name.

```
/tz
/tz Europe/Berlin
/tz Asia/Tehran
```

Replies: `Your time zone is Asia/Tehran.` followed by a hint, then `Time zone set to Europe/Berlin.` For a name the bot does not know: `Unknown time zone “Tehran”. Use a name like Europe/Berlin or Asia/Tehran.` New users start with `TODO_DEFAULT_TZ`. Times you type, and times the bot shows you, use your zone.

### `/digest HH:MM` and `/digest off`

Sets when **this chat** gets its daily digest, in your time zone. In a private chat that is your own list. In a group it is the group's list.

```
/digest 08:30
/digest ۰۸:۳۰
/digest off
/digest خاموش
```

Replies: `Daily digest set for 08:30 (Asia/Tehran).`, `Daily digest turned off.` and, for a bad time, `Invalid time. Use HH:MM, for example /digest 08:30, or /digest off.` With no argument it shows the usage. The digest is written in the language and calendar of the user who set it.

### `/lang [en|fa]`

Shows buttons to choose your language, or sets it directly. It also accepts `english`, `farsi`, `persian`, `فارسی` and `انگلیسی`.

```
/lang
/lang fa
/lang en
```

Reply: `Language set to English.` / `زبان روی فارسی تنظیم شد.` The choice is per user. New users start with Farsi (and the Jalali calendar) if their Telegram app language is Farsi, and with English (and the Gregorian calendar) otherwise.

### `/calendar [gregorian|jalali]`

Shows buttons to choose your calendar, or sets it directly. It also accepts `jalaali`, `shamsi`, `شمسی`, `جلالی` and `میلادی`.

```
/calendar
/calendar jalali
/calendar gregorian
```

Reply: `Calendar set to Jalali.` / `تقویم روی شمسی تنظیم شد.`

The calendar changes how dates are **shown** and how numeric dates and "every month on the Nth" are **read**. It does not change when anything fires: times are stored as exact instants.

How dates look, for the same moment (Thursday 15 Oct 2026, 08:15):

| Language and calendar | Shown as |
| --- | --- |
| English, Gregorian | `Thu 15 Oct 08:15` |
| English, Jalali | `Thu 23 Mehr 08:15` |
| Farsi, Gregorian | `پنج‌شنبه ۱۵ اکتبر ۰۸:۱۵` |
| Farsi, Jalali | `پنج‌شنبه ۲۳ مهر ۰۸:۱۵` |

Today and tomorrow are shown as words (`today 18:00`, `فردا ۱۸:۰۰`). A date in another year includes the year: `13 Oct 2027 08:15`, `۲۱ مهر ۱۴۰۶ ۰۸:۱۵`.

## Buttons

### List buttons

Each item in `/list` has four buttons.

| Button | What it does |
| --- | --- |
| `✅ 3` | Marks item 3 done. The list is redrawn with a "Done" line above it. |
| `✏️` | Asks you to **reply** to a message with the new text. |
| `⏰` | Asks you to **reply** with a time, such as `tomorrow 18:00`, `in 2h` or `فردا ۱۸:۰۰`. |
| `🗑` | Asks "Delete “…”?" with [🗑 Delete] and [Cancel]. |

With several pages there is a row `‹` `›` at the bottom. It changes the page by editing the same message.

For ✏️ and ⏰, you must use Telegram's **Reply** on the bot's question; only the person who pressed the button can answer, and the question expires after 10 minutes. If the answer is not understood, the bot explains and asks again. A recurring phrase is not accepted for ⏰.

If the item was already deleted, or belongs to another chat, the button answers "That item no longer exists or belongs to another chat."

### Reminder buttons

When a reminder fires you get a message like `⏰ buy milk` with these buttons:

| Button | What it does |
| --- | --- |
| `✅ Done` (`✅ انجام شد`) | Acknowledges the reminder. The message becomes `✅ buy milk`. |
| `10m` (`۱۰ دقیقه`) | Remind again in 10 minutes. |
| `1h` (`۱ ساعت`) | Remind again in 1 hour. |
| `Tomorrow` (`فردا`) | Remind again tomorrow at `TODO_DEFAULT_TIME`, in your time zone. |

After a snooze the message becomes `💤 Snoozed until today 09:25` (`💤 تا امروز ۰۹:۲۵ به تعویق افتاد`). A reminder that arrives more than two minutes late, for example after the bot was down, adds the line `late — was due …` (`با تأخیر — موعد: …`).

## Groups

- Add the bot to a group (BotFather `/setjoingroups` must be `Enable`). Only allowlisted users can use it there. Everyone else is ignored without any reply.
- A group has **one shared list**. Everyone allowed can add, finish, edit and delete its items. Item numbers belong to the group.
- In groups, **plain text is not added**. Use `/add` or reply to the bot.
- The bot answers a command by replying to it.
- With several bots in a group, write `/add@your_bot_name buy milk`. A command addressed to another bot is ignored. `/add` alone also works.
- `/lang`, `/calendar` and `/tz` are personal. They change how the bot shows things to **you**, not to the group. `/digest` is per chat.
- Reminders and digests are sent to the group, in the language and zone of the person who made the item or set the digest.
- If Telegram upgrades the group to a supergroup, the list moves with it.
- Keep the bot's privacy mode **on**. Commands and replies to the bot still arrive.
- `/whoami` is answered only in private chats.

## If you are not on the allowlist

The bot ignores you completely, in private chats and in groups, with one exception: in a private chat `/whoami` replies with your Telegram ID. Send it to the owner, who adds it to `TODO_ALLOWED_USERS` and restarts the service. Buttons pressed by non-allowlisted users do nothing.

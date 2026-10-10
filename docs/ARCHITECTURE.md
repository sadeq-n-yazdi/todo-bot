# Architecture

`todo-bot` is a self-hosted Telegram todo bot with reminders for a small group of
trusted users (a family). It speaks **English and Farsi** and shows dates in the
**Gregorian or Jalali** calendar. It is a single static Go binary with a single SQLite
file, and it runs as a hardened systemd service on a small VPS.

> **The contracts in §3 are frozen.** Changing them requires a `CONTRACT CHANGE`
> note in the PR description (see `CLAUDE.md`).

## 1. Library choices

| Concern | Choice | Why |
|---|---|---|
| Telegram | `github.com/go-telegram/bot` | It has no dependencies and is context-first. It supports long polling (`Start(ctx)`) and current Bot API versions. `WithServerURL` lets tests run against an `httptest` server. Handlers run asynchronously by default, so any shared state is guarded by a mutex. |
| Natural-language dates | `github.com/olebedev/when` (wrapped by `internal/nlparse`) | It is small, rule-based and pluggable, and it returns where in the input it matched, so the date can be stripped from the item text. Recurrence phrases use our own small grammar. |
| SQLite | `modernc.org/sqlite` | Pure Go, so it builds with `CGO_ENABLED=0` into a static binary that cross-compiles easily. |
| Time zones | `time/tzdata` import | Time-zone data is embedded in the binary, so it does not depend on the host (vps00 runs in UTC). |

## 2. Package layout

```
cmd/todo-bot/main.go          wiring; subcommands: run (default), version, migrate, backup <path>
internal/config               env config + slog setup
internal/clock                Clock interface, Real; internal/clock/clocktest.Fake
internal/calendar             Gregorian <-> Jalali conversion, month lengths
internal/i18n                 Lang, Locale{Lang,Cal}, catalog (Register/T), digits, month/weekday names
internal/model                plain domain types (no deps)
internal/recur                recurrence Rule: Parse/String/Next (DST-correct)
internal/nlparse              "/add" text -> item text + due + rule
internal/store                Store interface, ErrNotFound
internal/store/sqlite         implementation, embedded migrations, backup
internal/service              business logic; implements scheduler.Backend
internal/scheduler            single-goroutine loop
internal/tg                   Sender, Update, OutMessage (no Telegram library imported)
internal/tg/tgbot             adapter over go-telegram/bot
internal/tg/tgtest            FakeSender
internal/ui                   formatting, keyboards, callback-data codec
internal/handler              router, auth, commands, callbacks, reply-prompt flows
migrations/0001_init.sql      embedded via //go:embed
deploy/                       todo-bot.service, todo-bot.sysusers, env.example
scripts/deploy.sh
assets/                       icon.svg + PNG exports
```

The direction of dependencies is
`handler/scheduler → service → store, recur, nlparse → model`.
`ui` depends only on `model` and `tg`. The only packages that import a third-party library are
`tg/tgbot` (Telegram), `store/sqlite` (SQLite) and `nlparse` (dates).

## 3. Contracts

The Go source in `internal/*` is the authoritative version of these contracts. What follows is a summary.

- **clock**:
  - `Clock { Now() time.Time; After(d) <-chan time.Time }`.
  - `clocktest.Fake` adds `Set(t)` and `Advance(d)`; `Advance` fires any `After` channels whose time has come.
- **model**:
  - Domain types: `User`, `Chat`, `Todo`, `Reminder`, `DueReminder`, `TodoQuery`, `DigestView`.
  - Enums: `ChatKind` (private/group), `TodoStatus` (open/done/deleted), `ReminderKind` (due/custom/snooze), `ReminderState` (pending/sent/cancelled/failed).
- **recur**:
  - `Rule{Freq, Interval, Weekdays, MonthDay, Hour, Minute, Anchor}`.
  - `Parse(s)`, `String()` and `Next(after, loc)`.
  - The canonical string forms are:
    - `daily 09:00`
    - `weekly mon,wed 08:00`
    - `monthly 31 10:00`
    - `every 3d 2026-10-10 09:00`
- **nlparse**:
  - `Parser{DefaultHour, DefaultMinute}`.
  - `Parse(input, now) (Result, error)`, which returns `Result{Text, Due, Rule, HasTime}`.
  - `ParseWhen(input, now)`, used for reminder prompts.
- **store**: the `Store` interface, plus `ErrNotFound`. `WithTx(fn)` hands `fn` a store bound to the transaction, and that store is the only one `fn` may use.
- **tg**:
  - Types: `Message`, `Callback`, `Update`, `Button`, `OutMessage` (HTML text, inline keyboard, ForceReply).
  - `Sender{Send, Edit, AnswerCallback}`.
  - `APIError{Code, Desc, RetryAfter, MigrateTo}` and `IsPermanent(err)`.
- **ui**:
  - `Action{Kind, ID, Arg}` with `Encode` (at most 64 bytes) and `Decode`.
  - `TodoList`, `ReminderMsg`, `Digest` and `FormatWhen`.
- **service**: the `Service` interface, `Actor{UserID, ChatID, ChatKind, ChatTitle}`, and `Waker{Wake()}`.
- **scheduler**:
  - `Backend` is implemented by the service.
  - `New(b, sender, clock, log)`, plus `Run`, `Tick` and `Wake`.
- **handler**:
  - `Deps{Svc, Send, Parser, Clock, Allowed, BotUsername, Log}`.
  - `New(Deps)` and `Handle(ctx, Update)`.

## 3a. Languages and calendars

- **Locale.**
  - Each user has `lang` (`en`|`fa`) and `calendar` (`gregorian`|`jalali`).
  - On first contact the user gets `i18n.Default(from.language_code)`. A code starting with `fa` gives Farsi + Jalali; anything else gives English + Gregorian.
  - Users change these with `/lang` and `/calendar`.
- **Which locale a message is shown in.**
  - Replies use the sender's locale.
  - Reminders use the locale of the todo's creator.
  - Digests use the locale of whoever set them (`chats.digest_by`).
  - This is the same rule as for time zones.
- **Catalog.**
  - `i18n.Register(lang, map)` is called from an `init()` in a `messages*.go` file owned by each package.
  - `Locale.T(key, args...)` falls back first to English and then to the key itself. Duplicate keys panic.
- **Digits.**
  - On output, `Locale.Digits` / `Locale.Num` render Persian digits for Farsi. Command examples stay ASCII.
  - On input, `i18n.NormalizeDigits` converts Persian and Arabic-Indic digits to ASCII before parsing.
- **Calendar.**
  - `internal/calendar` ports the jalaali-js "breaks" algorithm. It is exact for Jalali years -61..3177, and the tests check every day from 1990 to 2100.
  - Storage and instants are always UTC and Gregorian. The calendar only affects:
    - how dates are displayed (`ui.FormatWhen`);
    - how numeric dates and "every month on the Nth" are parsed (`nlparse`);
    - monthly recurrence (`recur.Rule.Cal`). For example, `monthly 1 10:00 jalali` is the 1st of each Jalali month, and the 31st is clamped to 29 or 30 in shorter Jalali months.
  - Daily, weekly and every-N rules do not depend on the calendar.
- **Parsing.**
  - English and Farsi phrases are always accepted, whatever the user's language, because family members mix them.
  - Jalali month names (`مهر` / `Mehr`) always mean Jalali, and Gregorian month names always mean Gregorian.
  - Farsi `روزهای کاری` (workdays) means Saturday–Wednesday; English `weekdays` means Monday–Friday.
- **Rendering.**
  - Telegram lays out right-to-left text by itself, so messages are not wrapped in direction marks.
  - The Farsi zero-width non-joiner (ZWNJ) is written as `\u200c` in source code.
- **Commands.** Command names stay Latin, as Telegram requires. `setMyCommands` is called once with English descriptions and once with `language_code=fa` and Farsi descriptions.

## 4. Storage

- **Migrations** are files named `migrations/NNNN_*.sql`, embedded in the binary. The runner reads `PRAGMA user_version` and applies each newer file in its own transaction.
- **Times** are stored as INTEGER Unix seconds in UTC.
- **DSN:** `file:<path>?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-2000)`. The pool is limited to one connection (`SetMaxOpenConns(1)`).
- **Schema:** see `migrations/0001_init.sql`.
  - `users`: id, tz, lang and calendar.
  - `chats`: kind, title, `next_num` (per-chat item numbering), and digest settings (`digest_time`, `digest_by`, `digest_next_at`).
  - `todos`: num (per chat), text, status, `due_at`, `recur`, `tz`, `created_by`.
  - `reminders`: `todo_id`, `chat_id`, `fire_at`, kind, state, attempts, `message_id`.
  - There is a partial index on pending reminders, and a partial unique index that allows only one pending `due` reminder per todo.

**Which time zone is used:**
- A private chat's ID equals the user's ID.
- Each todo stores the time zone of the user who created it, and its recurrence is computed in that zone.
- A chat's digest stores the user who set it (`digest_by`), and that user's time zone and locale are used.
- Free-form input is always parsed in the time zone of the person typing.

## 5. Scheduler

There is one goroutine, and SQLite is the only source of truth. There is no in-memory heap, so nothing can fall out of sync.

```
Run: loop {
  Tick(now)
  next = NextWakeup()          // min(pending reminder fire_at, chat digest_next_at)
  d = clamp(next-now, 0, 5m)   // cap guards against suspend / wall-clock jumps
  select { ctx.Done | wake | clock.After(d) }
}
```

Startup runs `Tick` straight away, which is how missed reminders are caught up.

`Tick` processes due reminders in batches of 50. For each one:
1. If the todo is done or deleted, the reminder is cancelled.
2. Otherwise it sends `ui.ReminderMsg` with the buttons [Done] [10m] [1h] [Tomorrow].
3. It then calls `ReminderSent`. In one transaction this marks the reminder as sent. If the reminder is a recurring `due` reminder, the same transaction also sets the todo's `due_at = rule.Next(max(now, fire_at))` and inserts the next `due` reminder.

**Delivery is at-least-once.** A crash between sending a reminder and marking it sent means it is sent again.

**Send errors:**
- A 429 means waiting for the `retry_after` the API returned.
- A transient error reschedules the reminder at `now + 2^attempts` minutes. After 5 attempts it is marked `failed`.
- A permanent error (403, chat not found) marks it `failed` immediately.
- A 400 that carries a migrate ID calls `MigrateChat` and then retries.

**Catch-up policy:**
- One-off reminders are never dropped. If one is sent more than 2 minutes late, the message says "late — was due …".
- If several occurrences of a recurring reminder were missed, they are collapsed into one late message, and the next occurrence is computed from now.
- A digest that would be more than 1 hour late is skipped and moved to the next day.

**DST:**
- `Next` works on local calendar dates and builds times with `time.Date(y, m, d, H, M, 0, 0, loc)`. It never adds Durations.
- Once the slot has passed, the next result is always on a strictly later local date. This means the repeated hour when clocks go back never fires twice.
- A time in the spring-forward gap is moved forward by Go's normalization, which is accepted.
- "Every N days" counts calendar days from the anchor date.
- A monthly rule for the 29th, 30th or 31st falls on the last day of shorter months.

**Snooze:** 10m and 1h are added to now. "Tomorrow" means tomorrow at `TODO_DEFAULT_TIME` in the user's time zone.

**Done on recurring items:**
- `AckReminder` only edits the message to "✅ done", because the scheduler has already advanced the todo.
- `Complete` from the list advances the item only if `due_at <= now+12h`. Otherwise it just shows the next due date.
- Deleting a recurring item stops it.

## 6. Commands and UX

| Command | Behaviour |
|---|---|
| `/start`, `/help` | Introduction and the command list. |
| `/whoami` | Replies with your Telegram user ID. This is the **only** command that answers non-allowlisted users, and only in private chats. |
| `/add <text> [when]` | Examples: `buy milk tomorrow 18:00`, `pills every day 9am`, `rent every month on 1st 10:00`, `standup weekdays 9:30`, `call mom in 2h`. In a private chat, plain text without a command also adds an item. |
| `/list [page]` | Ten items per page, each shown as `3. buy milk — today 18:00 🔁`. Each item has the buttons [✅ 3] [✏️] [⏰] [🗑], and the page has [‹] [›]. Moving between pages edits the message in place. |
| `/done <n>`, `/del <n>`, `/edit <n> <text>` | Act on an item by its number in that chat. |
| `/remind <n> <when>` / `/remind <text> <when>` | Add a reminder to an existing item, or create an item that has a reminder. |
| `/today` | Shows the digest (overdue items and today's items) on demand. |
| `/tz [Area/City]`, `/digest [HH:MM\|off]` | Shows or sets your time zone, and the chat's digest time. |
| `/lang [en\|fa]`, `/calendar [gregorian\|jalali]` | Shows or sets your language and calendar. Farsi examples: `خرید نان فردا ساعت ۱۸`, `اجاره هر ماه ۱ام ساعت ۱۰`, `جلسه ۱۵ مهر ساعت ۹`. |

**Buttons and prompts:**
- The ✏️ and ⏰ buttons send a ForceReply prompt. The pending action is kept in a map guarded by a mutex, keyed by (chatID, promptMsgID), with a 10-minute TTL. It is lost on restart, which is accepted.
- 🗑 asks for confirmation before deleting.

**Groups:**
- Only allowlisted users can act. Anyone else is ignored silently.
- Callbacks check that `todo.chat_id == callback.ChatID`.
- `/cmd@BotName` is treated as `/cmd`.
- BotFather privacy mode stays **enabled**, because commands and replies to the bot are still delivered.
- When a group is upgraded to a supergroup, the `migrate_to_chat_id` service message triggers `MigrateChat`.

**Extracting the date in `/add`:**
- Dates are taken only from the **end** of the input, optionally after "at", "on" or "in". So "fix 2 bugs" and "monday meeting notes" are kept as plain text.
- A time with no date that has already passed today rolls to tomorrow.
- A date with no time uses `TODO_DEFAULT_TIME`.
- A due time creates a reminder.

## 7. Configuration (environment, `/etc/todo-bot/env`, root:root 0600)

```
TELEGRAM_TOKEN=123:abc                 # required
TODO_ALLOWED_USERS=11111,22222         # required, comma-separated int64
TODO_DEFAULT_TZ=Europe/Berlin          # default UTC; validated with time.LoadLocation
TODO_DEFAULT_TIME=09:00                # used for a date with no time, and for snooze "tomorrow"
TODO_DB_PATH=/var/lib/todo-bot/todo.db # default $STATE_DIRECTORY/todo.db, else ./todo.db
TODO_LOG_LEVEL=info                    # debug|info|warn|error; slog text to stderr
```

If `GOMEMLIMIT` is unset, `main` sets `debug.SetMemoryLimit(40 << 20)`.

## 8. Deployment

See [DEPLOYMENT.md](DEPLOYMENT.md).
- **Build:** a static binary is cross-built locally with `CGO_ENABLED=0 -trimpath`.
- **Copy:** `scp` puts it in `/opt/todo-bot`.
- **User and data:** a dedicated `todo-bot` user is created through `sysusers.d`, and data lives in `StateDirectory=todo-bot`.
- **Unit:** it is hardened with `ProtectSystem=strict` and `SystemCallFilter=@system-service`. `SystemCallErrorNumber=EPERM` is set as well; it is mandatory, because without it a blocked syscall kills the process with SIGSYS. Memory is capped with `MemoryMax=96M` and `GOMEMLIMIT=40MiB`.
- **`scripts/deploy.sh` subcommands:**
  - `bootstrap`
  - `update`: backs up the DB, keeps the previous binary as `.prev`, restarts and does a health check
  - `rollback`
  - `status`
  - `logs`

**Use a separate dev bot token locally.** Two processes polling with the same token get 409 Conflict.

## 9. CI

The `.github/workflows/ci.yml` workflow runs `go vet`, the linter, `go test -race ./...` and `make build-linux` on PRs and on `main`. A tag workflow attaches linux amd64 and arm64 binaries to the GitHub release.

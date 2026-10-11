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
internal/store                Store interface, ErrNotFound, provider registry (Register/Open by URL scheme), Backuper/Migrator
internal/store/sqlstore       shared SQL for all providers + migration runner, behind a small Dialect (#25)
internal/store/sqlite         SQLite Dialect + Open + Backup; registers "sqlite"
internal/store/storetest      conformance suite every provider must pass (#26)
internal/store/{postgres,mysql}  v0.2 providers
internal/lease                leader-lease loop (which node polls Telegram) (#27)
internal/service              business logic; implements scheduler.Backend
internal/scheduler            single-goroutine loop
internal/tg                   Sender, Update, OutMessage (no Telegram library imported)
internal/tg/tgbot             adapter over go-telegram/bot
internal/tg/tgtest            FakeSender
internal/ui                   formatting, keyboards, callback-data codec
internal/handler              router, auth, commands, callbacks, reply-prompt flows
migrations/<dialect>/NNNN_*.sql  embedded via //go:embed; same versions in every dialect
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
  - Assignment (§11): `User.Username`, `Todo.AssigneeID` (0 = none) and the read-only `Todo.AssigneeName`.
  - Wizard (§12): `Draft`, `DraftData`, `DraftStep` (`Step*`), the `Repeat*` choices, and `User.KBVersion` / `Prefs.KBVersion` (the reply keyboard).
- **recur**:
  - `Rule{Freq, Interval, Weekdays, MonthDay, Month, Cal, Hour, Minute, Anchor}`.
  - `Parse(s)`, `String()` and `Next(after, loc)`.
  - The canonical string forms are:
    - `daily 09:00`
    - `weekly mon,wed 08:00`
    - `monthly 31 10:00`
    - `every 3d 2026-10-10 09:00`
    - `yearly 02-29 09:00` and `yearly 12-30 09:00 jalali` (§12). A day the month lacks in a given year falls on its last day.
- **nlparse**:
  - `Parser{DefaultHour, DefaultMinute}`.
  - `Parse(input, now) (Result, error)`, which returns `Result{Text, Due, Rule, HasTime}`.
  - `ParseWhen(input, now)`, used for reminder prompts.
- **store**: the `Store` interface, plus `ErrNotFound`. `WithTx(fn)` hands `fn` a store bound to the transaction, and that store is the only one `fn` may use. Assignment (§11) adds `SetUsername`, `UserByUsername` and `ListAssigned`. The wizard (§12) adds `ListNamedUsers`, `SetKBVersion` and the compare-and-set draft methods `PutDraft`, `GetDraft`, `DraftByMessage`, `UpdateDraft` and `DeleteDraft`; `Purge` also deletes expired drafts.
- **tg**:
  - Types: `Message`, `Callback`, `Update`, `Button`, `OutMessage` (HTML text, inline keyboard, ForceReply, and a persistent reply keyboard or its removal (§12)).
  - `Message.FromUsername` and `Callback.FromUsername` carry the sender's @username without "@", as Telegram sends it (§11).
  - `Sender{Send, Edit, AnswerCallback}`.
  - `APIError{Code, Desc, RetryAfter, MigrateTo}` and `IsPermanent(err)`.
- **ui**:
  - `Action{Kind, ID, Arg}` with `Encode` (at most 64 bytes) and `Decode`.
  - `TodoList`, `ReminderMsg`, `Digest` and `FormatWhen`.
  - `Mention(userID, username, locale)` renders a `tg://user?id=` link labelled `@name`, or a translated label when the username is unknown (§11).
  - Wizard (§12): the `ActWiz*` kinds and `Wiz*` arguments, `WizardView`/`WizardCard`, `DayCell`/`MonthGrid`/`FirstWeekday`, the "➕ New task" helpers and the argument helpers (`DateArg`, `MonthArg`, `TimeArg` and their parsers).
- **service**: the `Service` interface, `Actor{UserID, ChatID, ChatKind, ChatTitle}`, and `Waker{Wake()}`. Assignment (§11) adds `NoteUsername`, `UserByUsername`, `AddTodoFor` and `ListAssigned`. The wizard (§12) adds `StartDraft`, `Draft`, `DraftByMessage`, `UpdateDraft`, `EndDraft`, `SaveDraft`, `NamedUsers`, `SetKBVersion`, `ErrStaleDraft` and `Config.DraftTTL`.
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
  - Reminders use the locale of the todo's creator. A reminder of an assigned item that goes to the assignee's private chat uses the assignee's locale and time zone (§11).
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

- **Migrations** are files named `migrations/<dialect>/NNNN_*.sql`, embedded in the binary. The runner records applied versions in a `schema_migrations` table and applies each newer file in its own transaction. It holds a migration lock while it runs, so two nodes starting at the same time don't race. Until #25 lands, SQLite still tracks versions with `PRAGMA user_version`, and #25 seeds `schema_migrations` from it.
- **Times** are stored as INTEGER Unix seconds in UTC.
- **DSN:** `file:<path>?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-2000)`. The pool is limited to one connection (`SetMaxOpenConns(1)`).
- **Schema:** see `migrations/sqlite/0001_init.sql`, `0002_multinode.sql` (§9), `0003_assign.sql` (§11) and `0004_wizard.sql` (§12).
  - `users`: id, tz, lang, calendar, `username` (unique where not null) and `kb_version` (the reply keyboard last sent, §12).
  - `chats`: kind, title, `next_num` (per-chat item numbering), and digest settings (`digest_time`, `digest_by`, `digest_next_at`).
  - `todos`: num (per chat), text, status, `due_at`, `recur`, `tz`, `created_by`, `assignee_id`.
  - `reminders`: `todo_id`, `chat_id`, `fire_at`, kind, state, attempts, `message_id`.
  - There is a partial index on pending reminders, and a partial unique index that allows only one pending `due` reminder per todo.
  - `drafts`: one wizard draft per (chat_id, user_id): the card's `msg_id`, `ask_msg_id`, `step`, the answers as JSON in `data`, the compare-and-set `version`, `updated_at` and `expires_at` (§12).

**Which time zone is used:**
- A private chat's ID equals the user's ID.
- Each todo stores the time zone of the user who created it, and its recurrence is computed in that zone.
- A chat's digest stores the user who set it (`digest_by`), and that user's time zone and locale are used.
- Free-form input is always parsed in the time zone of the person typing.

## 5. Scheduler

Each process runs one scheduler goroutine, and the database is the only source of truth. There is no in-memory heap, so nothing can fall out of sync. Every node runs a scheduler. Rows are **claimed** before delivery, so each reminder and digest is handled by one node at a time (§9).

```
Run: loop {
  Tick(now)
  next = NextWakeup()          // min over claimable rows of max(fire_at, claim_until)
  d = clamp(next-now, 0, max)  // max = WithMaxSleep, default 5m (30s multi-node)
  select { ctx.Done | wake | clock.After(d) }
}
```

Startup runs `Tick` straight away, which is how missed reminders are caught up.

`Tick` claims due reminders in batches of 50 (`ClaimReminders`). For each one:
1. If the todo is done or deleted, the reminder is cancelled (`CancelReminder`, conditional on the claim).
2. Otherwise it sends `ui.ReminderMsg` with the buttons [Done] [10m] [1h] [Tomorrow].
3. It then calls `ReminderSent`. In one transaction this marks the reminder as sent (`MarkReminderSent`, conditional on `state='pending'`). Only if that transition wins, and the reminder is a recurring `due` reminder, the same transaction also sets the todo's `due_at = rule.Next(max(now, fire_at))` and inserts the next `due` reminder.

**Delivery is at-least-once.** A crash between sending a reminder and marking it sent means it is sent again once the claim expires (ClaimTTL, 2m). Failure and cancel writes are conditional on the claim token, so a node whose claim has expired can never set a reminder another node already sent back to pending.

**Send errors:**
- A 429 is waited out inline only while `retry_after` is shorter than ClaimTTL/2 (`WithClaimTTL`). Otherwise the reminder is handed back with `ReminderFailed(permanent=false)`.
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
- A time in the spring-forward gap is moved **forward**, so 02:30 becomes 03:30. Go's `time.Date` would move it backward to the time before the jump, which `Next` corrects.
- Every-N-days is computed with integer arithmetic over Unix days. Anchors must be in 1900..2200 and intervals in 1..36500.
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
| `/add [@user] <text> [when]` | A leading `@user` assigns the item (§11). Examples: `buy milk tomorrow 18:00`, `pills every day 9am`, `rent every month on 1st 10:00`, `standup weekdays 9:30`, `call mom in 2h`. In a private chat, plain text without a command also adds an item. |
| `/list [page]` | Ten items per page, each shown as `3. buy milk — today 18:00 🔁`. Each item has the buttons [✅ 3] [✏️] [⏰] [🗑], and the page has [‹] [›]. Moving between pages edits the message in place. |
| `/done <n>`, `/del <n>`, `/edit <n> <text>` | Act on an item by its number in that chat. |
| `/remind <n> <when>` / `/remind <text> <when>` | Add a reminder to an existing item, or create an item that has a reminder. |
| `/today` | Shows the digest (overdue items and today's items) on demand. |
| `/tz [Area/City]`, `/digest [HH:MM\|off]` | Shows or sets your time zone, and the chat's digest time. |
| `/lang [en\|fa]`, `/calendar [gregorian\|jalali]` | Shows or sets your language and calendar. Farsi examples: `خرید نان فردا ساعت ۱۸`, `اجاره هر ماه ۱ام ساعت ۱۰`, `جلسه ۱۵ مهر ساعت ۹`. |

**Buttons and prompts:**
- The ✏️ and ⏰ buttons send a ForceReply prompt. The pending action is persisted in the `prompts` table, keyed by (chatID, promptMsgID), with a 10-minute TTL (`PromptTTL`). It survives restarts and can be completed on any node. Only the user who opened the prompt can answer it (`TakePrompt` checks the user ID).
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
TODO_DB_URL=sqlite:///var/lib/todo-bot/todo.db  # default sqlite://$STATE_DIRECTORY/todo.db, else sqlite:todo.db
                                       # (TODO_DB_PATH=<file> still accepted, deprecated)
                                       # v0.2: postgres://u:p@h/db?sslmode=verify-full | mysql://u:p@h/db?tls=true
TODO_LOG_LEVEL=info                    # debug|info|warn|error; slog text to stderr
TODO_NODE_ID=vps00                     # default hostname; names this node in leases and claims
TODO_LEASE_TTL=30s                     # poller lease
TODO_CLAIM_TTL=2m                      # how long a node reserves a due reminder
TODO_SCHED_MAX_SLEEP=5m                # use ~30s with several nodes
TODO_MODE=polling                      # webhook from v0.2, with TODO_HTTP_ADDR, TODO_WEBHOOK_URL, TODO_WEBHOOK_SECRET
```

The binary does not set a memory limit itself. The systemd unit sets `GOMEMLIMIT=40MiB`; when running outside systemd, set it in the environment.

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

## 9. Pluggable storage and multi-node

v0.1 ships SQLite on one host, but its code paths are already multi-node. v0.2 adds Postgres, MySQL, webhook mode, and running the bot on vps00 and vps03 together. The work is tracked in issues #24–#36.

### Providers
- `store.Open(ctx, url)` picks a provider by URL scheme. Providers call `store.Register` in `init()`, and `main` blank-imports them.
- All shared SQL lives in `sqlstore`. Each provider supplies a `Dialect` for the few places SQL differs between databases:
  - placeholder rebinding (`?` becomes `$n` for Postgres);
  - insert-id;
  - upsert and insert-ignore syntax;
  - allocating a todo number;
  - the claim statements;
  - the migration lock;
  - detecting unique-key violations.
- `storetest.Run` is the conformance suite. Every provider must pass it, including its concurrency tests, which open several independent handles to one database.
- Optional capabilities are checked with type assertions:
  - `store.Backuper` is SQLite only. Other databases use `pg_dump` or `mysqldump`.
  - `store.Migrator`.

### Coordination primitives (all in the database)

| Problem | Mechanism |
|---|---|
| Two schedulers sending the same reminder | `ClaimReminders` / `ClaimDigests` atomically stamp `claim_token` and `claim_until` (2m), in one statement or one transaction (MySQL). The token is unique per call. Claimed rows are invisible to other nodes until the claim expires, and `NextWakeup` counts them from `claim_until` |
| A node whose claim expired undoing another node's work | `RescheduleClaimed` / `AdvanceDigest` write only `WHERE claim_token = <mine>` (and `state='pending'`); `ok=false` means the claim was lost, so the write is dropped |
| A recurring todo advanced twice | `MarkReminderSent` is a conditional pending→sent update; only the winner advances the rule |
| Two nodes calling `getUpdates` (409) | lease `"poller"`, renewed every TTL/3 (TTL 30s). The holder is `NodeID:random`, one per process. The epoch is bumped on every acquisition that is not a live renewal. A node with no successful renewal for TTL/2 fences itself. `ReleaseLease` sets `held_until` to 0 |
| An update delivered twice (failover, webhook retry) | `handler.Handle` is the single gate for every transport: it calls `MarkUpdate(update_id)` before acting, so each update is handled at most once. If recording fails, `Handle` returns an error and does nothing: the webhook answers 500 so Telegram retries, and the poller logs it. Ids are purged after 48h |
| Prompts lost on restart or answered on another node | `prompts` table; `TakePrompt(chat, msg, user)` deletes the row, so exactly one caller wins. Another user's reply leaves the prompt in place, and expired rows are left for `Purge` |
| Two presses of one wizard card handled on two nodes (§12) | `drafts.version`: `UpdateDraft` and `DeleteDraft` apply only `WHERE version = <the version read>` and bump it, so exactly one wins; the loser changes nothing and tells the user. Callback data carries the version, so a press on an out-of-date card is detected |

- **Every node runs the scheduler.** Claims make that safe.
- **Delivery stays at-least-once.** A reminder is sent twice only if a node dies after sending it but before marking it sent, and its claim then expires.
- **Lease and claim expiry are compared on application clocks,** not database time. That keeps tests on the fake clock, but every node needs NTP (chrony) with skew of 1s or less.

### Modes
- **Polling** (v0.1, the default): only the lease holder polls. When a node wins the lease it calls `DeleteWebhook`, then `SetCommands` (en and fa), then runs `Poller().Run`. On SIGTERM it stops polling and calls `ReleaseLease`.
- **Webhook** (v0.2): every node serves `POST /tg` and `/healthz` behind Caddy with TLS. We use our own `http.Handler`, not the library's (go-telegram/bot v1.27.0 returns 200 even on a bad secret). It checks the secret in constant time, handles the update synchronously, and returns 500 only when `MarkUpdate` fails, so Telegram retries. The `"setup"` lease calls `SetWebhook` and `SetCommands` once per epoch.
- Running some nodes in one mode and others in the other is not supported.

### Schema additions (`0002_multinode.sql`)
- `reminders` gets `claim_token` and `claim_until`.
- `chats` gets `digest_claim_token` and `digest_claim_until`.
- New tables:
  - `leases(name PK, holder, held_until, epoch)`. The column is not called `until`, because that is reserved in MySQL 8;
  - `prompts(chat_id, msg_id, user_id, kind, todo_id, expires_at, PRIMARY KEY (chat_id, msg_id))`;
  - `processed_updates(update_id PK, seen_at)`;
  - `schema_migrations(version PK, name, applied_at)`.
- Postgres and MySQL use BIGINT for ids and times.
- MySQL has no partial indexes, so it emulates the partial unique index ("one pending `due` reminder per todo") with a unique stored generated column.
- The v0.2 providers must also ship `0003_assign.sql` (§11). The column types differ from SQLite:
  - **MySQL:**
    - `users.username` is `VARCHAR(32) NULL`, because a `TEXT` column cannot have a UNIQUE index without a prefix length. Telegram usernames are at most 32 characters.
    - A plain `UNIQUE (username)` is enough, because MySQL allows several NULLs in a unique index.
    - `todos.assignee_id` is `BIGINT NULL` with the same signedness as `users.id`, so that the foreign key is accepted.
    - The index on `(assignee_id, status)` is a plain, non-partial index.
  - **Postgres:**
    - `users.username` is `TEXT` (or `VARCHAR(32)`).
    - The partial unique index works as in SQLite. A plain `UNIQUE` also works, because NULLs never clash.
    - `todos.assignee_id` is `BIGINT REFERENCES users (id)`.
    - The index on `(assignee_id, status)` may stay partial.
- They must also ship `0004_wizard.sql` (§12), with the same table and indexes:
  - **Postgres:** `chat_id`, `user_id`, `version`, `updated_at` and `expires_at` are `BIGINT`; `msg_id` and `ask_msg_id` are `INTEGER` (or `BIGINT`); `step` and `data` are `TEXT`. `data` stays `TEXT`, not `JSONB`: nothing queries inside it, and the store reads and writes it as an opaque JSON string.
  - **MySQL:** the same, with `step VARCHAR(16)` and `data TEXT`. MySQL does not accept a literal default on a `TEXT` column, so drop the `'{}'` default (or use the expression default `('{}')`). The store always writes `data`.
  - No foreign keys, as in SQLite.
  - The same file adds `users.kb_version INTEGER NOT NULL DEFAULT 0` in every dialect.
  - **MySQL DSN:** set `clientFoundRows=true`. Without it, an `UPDATE` that writes the value already stored reports 0 affected rows, and `SetKBVersion`, `SetUsername` and other `checkAffected` writes would return `ErrNotFound`. The `KBVersion` conformance case writes the same value twice to catch this.
- The SQLite DSN adds `_txlock=immediate`, so that two handles on one file wait for each other instead of failing with `SQLITE_BUSY`.

## 10. CI

The `.github/workflows/ci.yml` workflow runs `go vet`, the linter, `go test -race ./...` and `make build-linux` on PRs and on `main`. A tag workflow attaches linux amd64 and arm64 binaries to the GitHub release.

From v0.2, a separate `db` job runs the store conformance suite against `postgres:17` and `mysql:8.4` service containers (#32). The main job stays SQLite-only, so `make ci` needs no Docker.

## 11. Assigning an item to another member (#58)

`/add @wife pick up our son from school today 3pm` adds the item to the chat where it was typed, and sends its reminders to the assignee's private chat. The contracts below were frozen in "Add assignment contracts (#58, part 1)". The follow-up PRs listed at the end implement the behaviour.

### Data model (`0003_assign.sql`)
- `users.username`: the Telegram @username, **lowercase and without "@"**. NULL means unknown. A partial unique index (`WHERE username IS NOT NULL`) ensures that at most one user has a given name.
- `todos.assignee_id`: the user the item is assigned to. NULL means none. It references `users (id)`.
- There is an index on `todos (assignee_id, status) WHERE assignee_id IS NOT NULL`, for `ListAssigned`.
- In Go:
  - `model.User.Username` is `""` when unknown.
  - `model.Todo.AssigneeID` is 0 when there is no assignee. This follows the convention of `Chat.DigestBy`.
  - `model.Todo.AssigneeName` is the assignee's current username. The store reads it live from `users` on every todo read, including `ClaimReminders`. It is read-only, so `InsertTodo` and `UpdateTodo` ignore it.
- `reminders.chat_id` stays the **creating chat**. The delivery target is decided when the reminder is sent (see "Scheduler" below). As a result, `MigrateChat`, `CancelPending` and the 403 fallback need no change, and reassigning an item would never have to rewrite its reminders.
- `UpdateTodo` writes `assignee_id`. A recurring item therefore keeps its assignee when `advanceRecurring` or `SetTZ` rewrites it, and `MigrateChat` keeps it too.
- `UpsertUser` never writes `username`. Only `SetUsername` does. This means `SetLang`, `SetTZ` and `ensureUser` cannot erase a name.

### Store API
```go
SetUsername(ctx, userID int64, name string) error            // lowercases, drops "@"; "" clears; ErrNotFound if no user
UserByUsername(ctx, name string) (model.User, error)         // case-insensitive; ErrNotFound if none or name == ""
ListAssigned(ctx, userID int64) ([]model.Todo, error)        // open, every chat; (due_at IS NULL, due_at, chat_id, num)
```

### Learning usernames
- The Bot API cannot turn `@name` into a user ID, so the bot learns names from the updates it sees. The `tgbot` adapter fills `tg.Message.FromUsername` and `tg.Callback.FromUsername` with the raw value.
- For every update from an allowlisted user, `handler.newReq` calls `Service.NoteUsername(userID, FromUsername)`.
  - It compares the name with the **database row**, never with an in-memory cache (§9), and calls `SetUsername` only when the name differs.
  - An empty `FromUsername` clears the stored name, because the user removed their @username.
  - A failure there is logged at warn level, and handling of the update carries on.
- **Reassignment rule:** Telegram usernames can move between accounts. `SetUsername` therefore clears the name from any other user in the same transaction. The new account wins, and the old account stops resolving until it sends a message with a new name.

### Syntax (handler)
- The **handler** splits off the mention, **before** `nlparse.Parse`. The service never sees `@name` text.
- The first token of `/add` arguments (and of plain text in a private chat, which also adds an item) is a mention when it matches `@[A-Za-z0-9_]{4,32}` and is followed by more text. The text has already been through `i18n.NormalizeDigits`.
  - A leading mention of the bot itself (`@<BotUsername>`, case-insensitive) is dropped and treated as "no assignee".
  - An `@word` anywhere else stays plain item text.
- To resolve a mention, the handler calls `Service.UserByUsername(name)` and then checks `Deps.Allowed[u.ID]`.
  - If the name is unknown or the user is not allowlisted, the handler replies in the sender's language with "@name hasn't started the bot yet. Ask them to open @<bot> and press Start." It creates nothing.
- Otherwise the handler calls `Service.AddTodoFor(a, u.ID, result)`.
  - `AddTodo` is the same call with assignee 0.
  - An assignee equal to `a.ChatID` (the assignee's own private chat) stores no assignee.
  - Assigning to yourself from a group is allowed: the reminder then goes to you privately.
- The "added" confirmation in the creating chat names the assignee (`→ @wife`).

### Visibility and authorisation
- The item stays on the creating chat's list. List rows of assigned items show `→ @name` (`ui.Mention(t.AssigneeID, t.AssigneeName, locale)`).
- **Mentions link by user ID.** `ui.Mention` renders `<a href="tg://user?id=ID">@name</a>`, so a ping reaches the assignee even if the @username has since moved to an account that never talked to the bot (which the bot could not have noticed). With no known username the link text is a translated label (`ui.mention.noname`).
- `/list` in a **private chat** also shows a section headed "Assigned to me", built from `Service.ListAssigned(userID)`.
  - That list excludes items in the user's own private chat.
  - The section is not paged and has per-item buttons. It is **capped** (PR 3 picks the limit, about 20 items), followed by a translated "and N more" line, because a message is limited to 4096 characters and the inline keyboard has a size limit too.
  - It is not shown in group chats.
  - Assigned items cannot be addressed by number from the private chat (`/done 3` uses that chat's numbers). They are reached through their buttons, whose callback data carries global IDs.
- **Authorisation rule:** an item action is allowed when `t.ChatID == a.ChatID` (the actor is in the item's chat, as today) **or** `t.AssigneeID == a.UserID`. Otherwise it fails with `ErrForeignChat`.
  - This covers `Todo`, `Complete`, `Delete`, `Edit`, `Remind`, `Snooze` and `AckReminder`.
  - For reminder actions (`Snooze`, `AckReminder`) the check is made on the reminder's **todo**, not on `r.ChatID`. A reminder delivered by DM has `r.ChatID` set to the creating chat, while the button press comes from the assignee's chat.
  - Messages are always edited through the callback's own `(ChatID, MessageID)`, never through `reminders.message_id`. A DM-delivered reminder's `message_id` belongs to the assignee's chat.
- **"Did" notice:** when the assignee completes an item from a chat other than its own, the handler sends "✅ @wife did: …" to `t.ChatID`. This applies to `Complete` from the list button, `/done`, and `AckReminder` on a reminder.
  - The notice is in the creator's locale.
  - The name comes from `t.AssigneeName`, falling back to the sender's display name.
  - The handler decides this from the returned todo and the actor. No new service API is needed.
- Digests are unchanged. They show each chat's own items, and assigned items do not appear in the assignee's digest.
- **Accepted limitation:** removing an assignee from `TODO_ALLOWED_USERS` does not unassign their items. They keep getting DMs for items already assigned to them, and cannot act on them, because the handler ignores non-allowlisted users. The creator can delete the item, or reassign it once reassignment exists.

### Scheduler routing and the 403 fallback
- **Every reminder of an assigned item goes to the assignee** (owner's decision). This covers the `due` reminder, `/remind` custom reminders, and snoozes, even a snooze pressed in the creating chat on a fallback message. Routing depends only on `dr.Todo.AssigneeID`, never on the reminder's kind or on who created it.
- `scheduler.Backend` is **unchanged**. For each claimed `DueReminder`:
  1. If `dr.Todo.AssigneeID == 0`, the reminder is delivered as today.
  2. Otherwise the scheduler renders `ui.ReminderMsg` in the **assignee's** prefs (`Prefs(AssigneeID)`) and sends it to chat `AssigneeID`, the assignee's private chat. Recurrence is still computed in `todo.TZ`.
  3. If that send fails **permanently** (`tg.IsPermanent`: 403 blocked or never started, chat not found), the scheduler falls back **once**. It renders the reminder in the creator's prefs, prefixes it with `ui.Mention(dr.Todo.AssigneeID, dr.Todo.AssigneeName, creatorLocale)`, and sends it to `dr.ChatID`, the creating chat, through the usual `sendWithRetry` (migration and 429 handling).
     - If the fallback succeeds, the scheduler calls `ReminderSent(id, fallbackMsgID)`.
     - If the fallback fails, it calls `ReminderFailed` with that error's permanence.
  4. A **transient** error on the DM goes through the normal backoff (`ReminderFailed(permanent=false)`) and does not fall back.
- The fallback happens inside one claim, within the same budget (`end`). The scheduler keeps no "already fell back" state. Each new reminder, including the next occurrence of a recurring item, tries the DM first, so a user who starts the bot later begins to receive reminders by DM.

### Multi-node
- Usernames, assignees and the authorisation inputs live only in the database. Any node handles any update. The rule from §9 stays: coordinate only through claims, the lease, `MarkUpdate` and `TakePrompt`.
- `SetUsername` is one transaction, and the unique index guarantees one owner per name.
  - Called **on the pool** (outside `WithTx`), concurrent calls that give one name to different users must end with exactly one owner and **no error**. The conformance case `UsernameConcurrent` checks this, and `NoteUsername` always calls it this way.
  - SQLite serialises them (`_txlock=immediate`).
  - The Postgres and MySQL providers (v0.2) must retry their own transaction on a unique violation.
  - Called **inside** `WithTx`, it joins the outer transaction, which cannot be retried. A conflict may then surface as an error, and on Postgres it aborts the whole transaction. Do not call it inside a larger transaction.
- **Stale names under parallel delivery:** with webhooks, two updates from one user can be handled at once on different nodes, so a name change can be recorded in either order. Whatever is stored corrects itself on that user's next update, because `NoteUsername` compares with the database row each time.
- The DM attempt and the fallback both happen under the reminder's claim. The rest of delivery stays at-least-once (§5). A crash after the fallback but before `ReminderSent` repeats the whole sequence (DM, then fallback) after the claim expires.
- The "did" notice is sent by whichever node handled the button press. It is sent at most once per update, because `MarkUpdate` drops a redelivered update.

### Follow-up PRs
Each PR owns only the files listed. They are numbered in merge order, but PRs 2–4 can be written in parallel against fakes, because the contracts above are frozen.

1. **Contracts** (#58 part 1, this PR). It changes:
   - `migrations/sqlite/0003_assign.sql`;
   - `internal/model`, `internal/tg/tg.go`, `internal/tg/tgbot` (`FromUsername`);
   - `internal/store/store.go`;
   - `internal/store/sqlstore/{users,todos}.go`, implemented;
   - `internal/store/storetest/assign.go`, the conformance suite;
   - `internal/store/sqlite/sqlite_test.go` (version checks);
   - `internal/service/service.go` (interface) and the stubs in `internal/service/assign.go`;
   - `internal/ui/mention.go` and `internal/ui/messages_mention.go`;
   - stub methods in `internal/handler/fake_svc_12_test.go`;
   - this section.
2. **service: assignment.** Owns:
   - `internal/service/assign.go` (it replaces the stubs);
   - the authorisation helpers in `internal/service/todos.go` (`ownedTodo`, `reminderOf`, `AckReminder`, and `AddTodo` delegating to `AddTodoFor`);
   - new tests in `internal/service/assign_test.go`.

   Tests use a real SQLite database in `t.TempDir()` and `clocktest.Fake`. They cover:
   - `NoteUsername`: writes only on change, `""` clears, a name moving to another account;
   - `AddTodoFor`: the own-private-chat rule, an unknown assignee;
   - `ListAssigned`: excludes the own private chat;
   - the either-or authorisation for every action, including Snooze and Done on a reminder by the assignee;
   - a recurring assigned item keeping its assignee across `ReminderSent`, `Complete` and `SetTZ`.
3. **handler + ui: `/add @user`, "Assigned to me", "did" notice.** Owns:
   - `internal/handler/cmd_todo.go` and `messages_todo.go` (mention split, resolution, the "hasn't started the bot" reply, the `/list` section, `/done` notice);
   - `callbacks.go` (Done-button notices);
   - in `router.go`, only `newReq` and its two callers, to call `NoteUsername`. The exported handler API is unchanged, but `router.go` is a frozen file, so this PR needs a `CONTRACT CHANGE` line for it;
   - `internal/ui/render.go` and a new `internal/ui/messages_assign.go` (`→ @name` on rows, the assigned section);
   - new handler test files with their own fake service;
   - the existing fakes that embed `service.Service`, in `internal/handler/fake_svc_11_test.go` and `router_test.go` (`prefsOnly`, `prefsPanics`). They need `NoteUsername` (and, where used, `UserByUsername`) methods. Without them the call panics on the nil embedded interface, and `Handle` recovers the panic, so the tests fail confusingly instead of crashing.

   It registers every string in en and fa, with prefixes `todo.assign.*` and `ui.assign.*`.
4. **scheduler + e2e + docs: deliver to the assignee, 403 fallback.** Owns:
   - `internal/scheduler/run.go`, with tests in a new `internal/scheduler/assign_test.go` using the existing fake backend and `tgtest.FakeSender` with `ErrFn` returning a 403;
   - a new `internal/e2e/assign_test.go`. It uses two users and the fake clock. It checks that `/add @wife … today 3pm` in a group reaches the assignee's DM at 15:00, that the assignee's Done completes the item and posts the "did" notice to the group, that the creator still sees and completes the item, and that the 403 fallback posts once;
   - `docs/USAGE.md`, `README.md` and `README.fa.md`.

   Its e2e part needs PRs 2 and 3 merged first.

The v0.2 Postgres and MySQL providers ship their own `0003_assign.sql` and must pass the new conformance cases.

## 12. Step-by-step task creation wizard (#68)

The wizard creates an item with buttons and short answers, as an alternative to natural language, which keeps working unchanged. The decisions in issue #68 are final. The contracts below were frozen in "Add wizard contracts (#68, part 1)". The follow-up PRs listed at the end implement the behaviour.

### Entry points
All four start a new draft for (chat, user), replacing any earlier one:
- `/new` (any chat). The command is added to the `setMyCommands` menu in en and fa.
- `/add` with no text. It starts the wizard instead of showing the usage help.
- The **➕ New task** inline button under every `/list` page (`ui.NewTaskButton`, data `ws:0`).
- The persistent reply-keyboard button **➕ New task** in private chats (`ui.NewTaskKeyboard`, sent as `tg.OutMessage.ReplyKeyboard`). Pressing it sends its label as plain text. The handler recognises it in both languages with `ui.IsNewTaskLabel`, in `onWizardText`, **before** the private-chat rule that turns plain text into an item.
  - See "The reply keyboard" below for when it is sent.

### The reply keyboard
Telegram attaches a reply keyboard only to a **sent** message (`sendMessage`). It cannot be added by an edit or a callback answer, and a message carries one markup only, so a message with an inline keyboard or ForceReply cannot carry it. Once sent, it stays under the chat's input field until replaced or removed.
- **Who has it** is stored per user: `users.kb_version` (`0004_wizard.sql`, `model.User.KBVersion`, returned as `model.Prefs.KBVersion`). 0 means never sent. `ui.NewTaskKeyboardVersion` (1) is the current layout; bumping it re-sends the keyboard to everyone.
- **When.** In a **private** chat, while `r.Prefs.KBVersion != ui.NewTaskKeyboardVersion`, the first plain reply the bot sends to that message carries `ui.NewTaskKeyboard` in the user's language. After that send succeeds, the handler calls `Svc.SetKBVersion(user, ui.NewTaskKeyboardVersion)`. Never in groups: a reply keyboard there would show for every member.
- **Where.** In the handler's shared reply helper, `reply` in `router.go`, which `say` and every command answer go through. `handleMessage` marks the request (`newTaskKeyboardCtx`, a use-once pointer in `ctx` that lasts one update), and `reply` asks `offerNewTaskKeyboard` before each send. The mark is used once, and skipped for an `OutMessage` with `Keyboard`, `ForceReply` or `RemoveKeyboard`. Edits, callback answers and the scheduler's sends never carry it. Both hooks are in `handler/wizard.go`: stubs in part 1, implemented by PR 3.
- **Never on a message that will be edited.** Telegram cannot edit a message that carries a reply keyboard. Such a message is sent with an inline keyboard, which makes the hook skip it, or with `h.Send.Send` directly. The wizard card is the main case (see "Starting").
- **Always attached** to the `/start` and `/help` replies in private chats, even when the version matches. This lets a user who removed it get it back.
- **After a language change**, `/lang <lang>` in a private chat attaches the keyboard in the new language to its confirmation. The language buttons answer through an edit, which cannot carry a keyboard, so in a private chat the callback calls `Svc.SetKBVersion(user, 0)` and the next reply carries it.
- **Multi-node.** Two nodes answering two messages of one user at once may both attach it. That is harmless, and the version written is the same.

### Steps
The draft (`model.Draft`) holds the step and the answers (`model.DraftData`). Each row is one card. "Next" is where an answer goes.

| Step | Card | Buttons (callback) | Next |
|---|---|---|---|
| `text` | "What's the task?" | ✖ Cancel | `found` if the text has a date, else `date` |
| `found` | "Found: tomorrow 09:00 — use it?" | Yes (`wn:v:y`), No (`wn:v:n`), ◀ Back, ✖ Cancel | Yes: `assignee`; No: `date` |
| `date` | quick picks: Today, Tomorrow and the next 5 weekdays by name; 📅 Other date; No date | `wd:v:YYYYMMDD`, `wd:v:o`, `wd:v:n`; ⏭ Skip (= No date), ◀ Back, ✖ Cancel | a date: `time`; No date: `assignee` |
| `date`, grid view | month grid in the user's calendar | ◀ ▶ `wm:v:j140507`, day `wd:v:YYYYMMDD`, past day and padding `wz`, back to quick picks `wd:v:q` | as `date` |
| `time` | 09:00, 12:00, 15:00, 18:00, 20:00 (`ui.WizardTimes`); ⌨️ Type a time; Default | `wt:v:HHMM`, `wt:v:k`, `wt:v:d`; ⏭ Skip (= Default), ◀ Back, ✖ Cancel | `repeat`; Type: `time_text` |
| `time_text` | "Reply with a time: 15:30, 3pm, ۱۵:۳۰" | ◀ Back, ✖ Cancel | `repeat` |
| `repeat` | Once, Daily, Weekly (on the chosen weekday), Monthly (in the user's calendar), Yearly | `wr:v:o\|d\|w\|m\|y`; ⏭ Skip (= Once), ◀ Back, ✖ Cancel | `assignee` |
| `assignee` | group: Nobody, Me, then one button per member; private: Me, then one button per member | `wa:v:n` (Nobody, group only), `wa:v:m` (Me), `wa:v:<userID>`; ⏭ Skip (= Nobody in a group, Me in a private chat), ◀ Back, ✖ Cancel | `confirm` |
| `confirm` | summary in the user's language and calendar | ✅ Save `wn:v:v`, ✏️ Edit `wn:v:e`, ✖ Cancel | Save: done |
| `confirm`, edit menu | "What do you want to change?" | `we:v:text\|date\|time\|repeat\|assignee`, ◀ Back (to the summary) | the chosen step |

`v` is the draft's `Version`. Button data is built only with `ui.Encode` and the helpers `ui.DateArg`, `ui.MonthArg` and `ui.TimeArg`, and parsed with `ui.Decode` and `ui.ParseDateArg`, `ui.ParseMonthArg` and `ui.ParseTimeArg`. No argument contains `:`, and the longest data (`wa:<max int64>:<max int64>`) is 42 bytes.

**Skipped steps.**
- The text step stores the normalised text in both `Text` and `OrigText`.
- `found` is shown only when `nlparse.Parse` of the text (normalised with `i18n.NormalizeDigits`) returns a `Due`.
  - Yes sets `Text = FoundText`, keeps `FoundDue`, `FoundRule` and `FoundTime` as the date, and clears `Date`, `Time` and `Repeat`.
  - No clears the `Found*` fields and sets `Text = OrigText`, the whole original text.
  - Yes → Back (from `assignee`) → No therefore restores the date phrase.
- `time` and `repeat` are skipped when there is no date (No date, or a found date was used). Choosing No date clears `Time` and `Repeat`.
- The members are `NamedUsers(actor)` filtered by `Deps.Allowed`. The list is read from the store every time the step is drawn, never from memory.
- `assignee` is skipped in a **private** chat when there are no members, because Me is then the only choice. In a **group** it is always shown, because Nobody and Me differ there.
- **What the choices store** in `Data.AssigneeID`:
  - Nobody (`wa:v:n`) stores 0. The reminder goes to the group, as for `/add`.
  - Me (`wa:v:m`) stores the actor's own ID. In a group, Save calls `AddTodoFor` with assignee = actor, so the item stays on the group's list and its reminders come to the actor by DM (§11). In a private chat, `AddTodoFor`'s own-private-chat rule (§11) turns it into no assignee.
  - A member stores their ID.

  The summary shows Nobody, Me or `@name`. A skipped step stores the Skip value.

**Validation.**
- The text must not be empty; otherwise the step is asked again.
- A typed time goes through `i18n.NormalizeDigits` and accepts `15:30`, `3pm` and `۱۵:۳۰`. Anything else is asked again with a note.
- A date in the past (a stale quick pick after midnight, or a past grid day) is refused with a toast. A grid shows past days as `wz:v:p`, whose toast says the day has passed.
- A time on Today is not checked on the time step, because the repeat is not known yet. The check is made at **Save**, and only for a **once** item (see "Save"):
  - An explicit time that has passed sends the user back to `time` with a note.
  - Default after the default slot behaves like `/add <text> today`: due today at 23:59 with `HasTime=false`. The item is overdue at once and gets no reminder (`dateOnlySentinel` in the service).
- A **repeating** item is never refused for a past time. Save passes `Due` = the date at H:M, and the service advances it with `Rule.Next`, exactly as for `/add`. For example, `x today 08:00 every day` at 10:00 is first due tomorrow at 08:00.

**Back** goes to the previous step **that was shown**. That is computed from the data and the skip rules, not from a history: `found` → `text`; `date` → `text`; `time` → `date`; `time_text` → `time`; `repeat` → `time`; `assignee` → `repeat` if there is a date, otherwise `found` if a found date was used, otherwise `date`; `confirm` has no Back (◀ Back on the edit menu returns to the summary). Back does not clear the answer of the step it leaves, so going forward again shows it as the current choice.

**Edit** opens the edit menu. Picking a step sets `Data.Editing` and goes to it. With `Editing` set, the answer returns to `confirm` instead of going on, with two exceptions. A new date walks on through `time` and `repeat`, because both depend on it. A text edit does not run the date detection again: it sets `Text` and `OrigText` and keeps the chosen date. Back from an edited step returns to `confirm`. `Editing` is cleared on `confirm`.

**Cancel** works on every step. It ends the draft (`EndDraft`) and edits the card to "Cancelled".

**Month grid.**
- `ui.MonthGrid(cal, year, month, today)` lays out the weeks. Each row has 7 cells, starting on Saturday for Jalali and Monday for Gregorian (`ui.FirstWeekday`). Padding cells have `Day == 0`, and days before `today` are `Past`.
- Every cell carries its Gregorian civil date. A Jalali month is converted with `internal/calendar`, so Esfand has 30 days in a leap year.
- The card shows the month name and year in the user's language and digits (`Locale.MonthName`, `Locale.Num`). The header row shows the weekday names.
- ◀ and ▶ use `calendar.AddMonths` and cross year boundaries. ◀ is not shown for the current month, because every earlier day is in the past.
- The grid, its month and the edit menu are **views**. They are not stored: their buttons carry everything needed (`wm` carries the month), so a press only checks the version and edits the card.

### Text answers
- A text step (`text`, `time_text`) is answered by a message from the draft's user in the draft's chat that is either
  - a reply to the card (`MsgID`) or to the question (`AskMsgID`), or
  - in a **private** chat, any non-command text while the draft is on a text step (owner's decision). It is the wizard's answer and does **not** become an item. The reply-keyboard label is the exception: it starts a new wizard. Cancel, or the draft's expiry, ends this, and plain text becomes an item again. While the draft is on a button step, plain text is handled as before.
- In a **group** the answer must be a reply. Other messages are ignored by the wizard and handled as before; in a group that means nothing happens. To make replying easy, a text step in a group also sends a ForceReply question (`ui.WizardAsk`, stored as `AskMsgID`), because a message with an inline keyboard cannot carry ForceReply.
- `onWizardText` runs after `onReply` (prompts) and before command dispatch. It is reached only for non-commands. `/new` and the other commands are never taken as an answer.

### Button presses
`handler` checks, in order:
1. Decode the data; malformed data is ignored as today.
2. `ws:0` starts a new wizard; `wz` only answers the callback (the past-day toast for `wz:v:p`).
3. `Svc.DraftByMessage(cb.ChatID, cb.MessageID)`. `ErrNotFound` means the toast "This wizard has expired. Start again with /new.", and the card's keyboard is removed.
4. `draft.UserID != cb.FromID` means the toast "Only the person who started this can use it." Nothing changes.
5. `act.ID != draft.Version` means the toast "This card was out of date — here is the current step", and the card is redrawn from the stored draft.
6. Apply the choice to a copy of the draft and call `Svc.UpdateDraft`. `ErrStaleDraft` is handled like step 5. On success, edit the card with `ui.WizardCard` and the new `Version`.

Views (grid, month change, back to quick picks, edit menu, its ◀ Back) do steps 1–5 and then edit the card, without a write. They do not extend the expiry.

**Handler rules for PR 3** (data from a modified client is untrusted):
- **Group messages.** In a group, `onWizardText` returns `handled=false` **before** calling `Svc.Draft` unless `m.ReplyToID != 0`. Ordinary group chatter then costs no query.
- **Dates.** `wm` and `wd` arguments are clamped to a window: from the current month (or today) to 5 years ahead, in the user's calendar. Data outside it is answered with a plain callback answer and changes nothing.
- **Assignees.** `wa:<id>` is checked again at press time: `id` must be in `Deps.Allowed` and in `NamedUsers(actor)`. Otherwise the step is redrawn. Save checks it once more (see "Save").
- **The keyboard mark** set by `newTaskKeyboardCtx` is a use-once pointer in `ctx`. At most one message per update carries the reply keyboard.

**Starting.**
1. The handler sends the card with a **placeholder inline row**: one `ActWizNoop` button with data `wz:0`.
   - The card is edited at every step, and Telegram cannot edit a message that carries a reply keyboard.
   - The inline row makes `offerNewTaskKeyboard` skip the card (see "The reply keyboard").
2. In a group, the handler also sends `ui.WizardAsk` (the ForceReply question).
3. It calls `Svc.StartDraft` with the card's `MsgID` and the `AskMsgID`.
4. It edits the card to the first step's keyboard, which carries the new `Version`.

The version cannot be known before `StartDraft`, and the buttons must carry it. The old card of a replaced draft no longer finds a draft, so its buttons say "expired".

### Save
Save creates the item through the same code as `/add`, and ends the draft **in the same transaction** (`Svc.SaveDraft`).
1. Build an `nlparse.Result` (below) and validate it, still holding the draft:
   - **The assignee.** If `Data.AssigneeID` is a member who is no longer in `Deps.Allowed`, set it to 0, show the `assignee` step again with a note (`UpdateDraft`), and stop.
   - **A once item whose due time has passed** (no rule; the draft sat on `confirm`, or a time on Today) goes back with a note. A picked date goes back to `time`. A found date goes back to `date`, with the `Found*` fields cleared and `Text = OrigText`.
   - **The Today default.** For a once item with Default on Today after the default slot, use 23:59 with `HasTime=false`.
   - **Repeating items** are never refused (see "Validation").
2. `Svc.SaveDraft(draft, actor, Data.AssigneeID, result)`. In one `WithTx` it runs the conditional `DeleteDraft(version, now)` and then the body of `AddTodoFor`.
   - **`ErrStaleDraft`** (another press or node won, or the draft expired) is handled like a stale press. No item is created, so a double press or two nodes create the item **at most once**.
   - **Any other error** rolls the whole transaction back. The draft is still there, with the same version, and the user gets the generic error and can press Save again.
3. Edit the card into the same "added" confirmation `/add` sends, including `→ @name`.

`EndDraft` is only for Cancel.

The `Result`:
   - `Text` is `Data.Text`.
   - **Found date used:** `Due = FoundDue`, `Rule = recur.Parse(FoundRule)` (nil if ""), `HasTime = FoundTime`.
   - **No date:** `Due = nil`, `Rule = nil`, `HasTime = false`.
   - **A date:** `(y, m, d)` from `Data.Date` and `(H, M)` from `Data.Time`, or `Parser.DefaultHour/DefaultMinute` when it is "". `Due = time.Date(y, m, d, H, M, 0, 0, userTZ)` and `HasTime = Data.Time != ""`, also when that is in the past for a repeating item (the service advances it). Only for a once item with Default on today after the default slot, use 23:59, as in "Validation". `Rule` comes from `Data.Repeat`. The handler builds the canonical string (for example `weekly tue 15:00` or `yearly 12-25 15:00 jalali`) and gets the rule with `recur.Parse`, so a rule `recur` cannot read back is never stored:
     - once: nil;
     - daily: `{Freq: Daily, Cal: Gregorian, Hour: H, Minute: M}`;
     - weekly: `{Freq: Weekly, Weekdays: [weekday of the date], Cal: Gregorian, Hour: H, Minute: M}`;
     - monthly: `{Freq: Monthly, MonthDay: day of the date in the user's calendar, Cal: user's calendar, Hour: H, Minute: M}`;
     - yearly: `{Freq: Yearly, Month and MonthDay of the date in the user's calendar, Cal: user's calendar, Hour: H, Minute: M}`.

     These are the rules `nlparse` builds for the same phrases: `Cal` is Gregorian except for monthly and yearly rules.

**Equivalence (acceptance test).** The issue says text → Tomorrow → 15:00 → Weekly → @member → Save must create the same item as `/add @member text tomorrow 15:00 every week`. The criterion holds as written, and the e2e test compares against that original command. Both paths store text `text`, `due_at` tomorrow 15:00 and `weekly <tomorrow's weekday> 15:00`, because #71 makes `nlparse` start `<date> <time> every week|month` on that date at that time. The yearly form (`every year`) is the remainder of #70 (PR 5 below). The weekly criterion does not depend on it.

### Groups
- Only the user who started a wizard can press its buttons. Anyone else gets a toast (check 4 above).
- Text answers must be replies (see "Text answers").
- The item goes to the group's list (`Actor.ChatID` is the group), exactly as `/add` in that group.
- Several members can run wizards in one group at the same time, because the key is (chat, user).
- The `assignee` step offers Nobody (the reminder goes to the group) and Me (assigned to yourself, so the reminder comes to you by DM, as with `/add @me …`, §11), then the members.

### State, expiry and multi-node
- **The draft lives only in the database** (`drafts`, `0004_wizard.sql`). No node keeps wizard state in memory, so a restart or a failover mid-way loses nothing. Coordination is only through `MarkUpdate` (each update once) and the draft's version (each press once), in line with §9.
- **Version.** `PutDraft` sets a version greater than the replaced draft's. A new row (no draft stored) is seeded with `UpdatedAt` in Unix milliseconds, so a draft started after a delete or a purge does not reuse a version. Both `UpdateDraft` (which bumps the version) and `DeleteDraft` are `WHERE version = <read> AND expires_at > now`. Of two presses handled at once on two nodes, one wins; the other changes nothing and redraws the current card. Out-of-order `editMessageText` calls can briefly show an older card, but its buttons carry an older version, so pressing one redraws the current step.
- **Expiry.** A draft expires `DraftTTL` (30 minutes) after its last write. Every read and every conditional write (`UpdateDraft`, `DeleteDraft`, and so `EndDraft` and `SaveDraft`) treats an expired draft as missing, and `Purge` deletes it. Pressing a button on it gives the "expired" toast. View presses (grid, month change, edit menu) do not write, so they **do not** extend the expiry; only an answer does.
- **Mixed versions during a rolling upgrade.** A node that reads a draft whose `Step` it does not know treats it as expired: the "expired" toast, and the user starts again. `DraftData` fields it does not know are dropped when it writes the draft back. That is acceptable, because a new field only matters to the node that understands it, and the draft is short-lived.
- **Why JSON for the answers.** Keys and coordination fields are columns. The answers are one JSON object (`DraftData`), because nothing queries them, and a step added later only adds a field to `DraftData`: older rows decode with the zero value, and no migration is needed. Typed columns would need a migration in every dialect for each new field.
- **No foreign keys.** A group's `chats` row may not exist yet (`EnsureChat` runs inside `AddTodo`), and drafts must not block `MigrateChat`. After a supergroup migration, the old chat's drafts are simply left to expire.
- **Rollback and upgrade order.** A binary older than this PR cannot parse a `yearly …` rule. It logs "unusable rule" and stops repeating that item: the current occurrence still fires, but no next one is scheduled. So upgrade every node before anyone creates yearly items, and before rolling back, check for yearly items. The `drafts` table and `users.kb_version` are harmless to an older binary.

### Contracts frozen by part 1
- `migrations/sqlite/0004_wizard.sql`: the `drafts` table and `users.kb_version INTEGER NOT NULL DEFAULT 0`.
- `model.Draft`, `model.DraftData` (with `OrigText`), `model.DraftStep` (`Step*`) and the `Repeat*` constants; `model.User.KBVersion` and `model.Prefs.KBVersion`.
- `recur.Yearly` and `recur.Rule.Month`, implemented, with the canonical form `yearly MM-DD HH:MM [jalali]`.
- `store.Store`:
  ```go
  ListNamedUsers(ctx) ([]model.User, error)                                 // username IS NOT NULL, by username
  SetKBVersion(ctx, userID int64, v int) error                              // ErrNotFound if no user; UpsertUser never writes it
  PutDraft(ctx, d *model.Draft) error                                       // replace; sets d.Version (new row: UpdatedAt in Unix ms)
  GetDraft(ctx, chatID, userID int64, now time.Time) (model.Draft, error)   // ErrNotFound if none or expired
  DraftByMessage(ctx, chatID int64, msgID int, now time.Time) (model.Draft, error)
  UpdateDraft(ctx, d *model.Draft, now time.Time) (ok bool, err error)      // CAS on d.Version, live only; bumps it
  DeleteDraft(ctx, chatID, userID, version int64, now time.Time) (ok bool, err error) // CAS on version, live only
  ```
  `Purge` also deletes expired drafts. These are implemented in `sqlstore` and covered by the conformance cases `Drafts`, `DraftExpiry`, `DraftConcurrent`, `PutDraftConcurrent`, `ListNamedUsers` and `KBVersion`. Every user read returns `kb_version`. `PutDraft` replaces with an in-place `UPDATE … version = version + 1` (and inserts only a missing row), so it stays monotonic under read committed too.
- `service.Service`:
  ```go
  StartDraft(ctx, d *model.Draft) error                       // sets UpdatedAt, ExpiresAt (now+DraftTTL), Version
  Draft(ctx, chatID, userID int64) (model.Draft, error)
  DraftByMessage(ctx, chatID int64, msgID int) (model.Draft, error)
  UpdateDraft(ctx, d *model.Draft) error                      // ErrStaleDraft
  EndDraft(ctx, d model.Draft) error                          // Cancel; ErrStaleDraft
  SaveDraft(ctx, d model.Draft, a Actor, assigneeID int64, r nlparse.Result) (model.Todo, error) // Save; one tx
  NamedUsers(ctx, exceptID int64) ([]model.User, error)
  SetKBVersion(ctx, userID int64, v int) error                // creates the user if new
  ```
  The service also gets `ErrStaleDraft` and `Config.DraftTTL` (default 30m), and `Prefs` returns `KBVersion`. The draft methods are thin wrappers. `SaveDraft` runs the conditional `DeleteDraft` and the body of `AddTodoFor` (extracted as `addTodoTx`) in one `WithTx`. Its tests cover a stale version (no item), a failing insert (the draft is kept), two nodes saving one version (one item) and an expired draft.
- `tg.OutMessage.ReplyKeyboard [][]string` and `RemoveKeyboard bool`. At most one markup takes effect, with this precedence: inline keyboard, ForceReply, reply keyboard, remove. `Edit` ignores the last two. The `tgbot` adapter maps them (persistent, resized) and has tests. `tgtest.FakeSender` records the whole `OutMessage`, so they need no change there.
- `ui`:
  - the kinds `ActWizStart`, `ActWizDate`, `ActWizMonth`, `ActWizTime`, `ActWizRepeat`, `ActWizAssign`, `ActWizNav`, `ActWizEdit` and `ActWizNoop`, with the `Wiz*` arguments, in `Encode`/`Decode`. `ActWizAssign` takes `WizAssignNone` (`n`), `WizAssignMe` (`m`) or a positive user ID;
  - `NewTaskKeyboardVersion`;
  - `WizardView` (with `Group` and `AssigneeName`), `WizardCard(v, lc, loc, now) tg.OutMessage`, `WizardAsk(step, lc) tg.OutMessage` (the ForceReply question for groups, so that question strings live in ui only) and `WizardTimes`;
  - `DayCell`, `MonthGrid(cal, year, month, today calendar.Date) [][]DayCell` and `FirstWeekday(cal)`;
  - `NewTaskLabel`, `IsNewTaskLabel`, `NewTaskKeyboard` and `NewTaskButton`, implemented, with the key `ui.newtask.label` in en and fa;
  - `DateArg`/`ParseDateArg`, `MonthArg`/`ParseMonthArg` and `TimeArg`/`ParseTimeArg`, implemented.

  `WizardCard` and `MonthGrid` are stubs that return zero values. `WizardAsk` is a stub that returns only `ForceReply`.
- `handler`: `router.go` registers `/new` (`cmdNew`) and calls `onWizardText` for non-commands after `onReply`. `handleMessage` calls `newTaskKeyboardCtx`, and `reply` calls `offerNewTaskKeyboard` and, after a successful send, the function it returns. All four live in `wizard.go` as stubs: `/new` answers like an unknown command, `onWizardText` handles nothing, and the keyboard hooks attach nothing. The stubs call no new service method, so the existing fakes keep working.

### Follow-up PRs
Each PR owns only the files listed. They are numbered in merge order, but PRs 2–4 can be written in parallel against the frozen contracts.

1. **Contracts** (#68 part 1, this PR). It changes:
   - `migrations/sqlite/0004_wizard.sql`;
   - `internal/model/model.go`, `internal/recur` (`Yearly`, with `yearly_test.go`);
   - `internal/store/store.go`, `internal/store/sqlstore/{drafts,users,multinode}.go` and `internal/store/storetest/{wizard,suite}.go`;
   - `internal/service/service.go`, `internal/service/wizard.go` (with `wizard_test.go`), and `addTodoTx` extracted in `internal/service/todos.go`;
   - `internal/tg/tg.go` and `internal/tg/tgbot` (reply keyboard);
   - `internal/ui/{ui,codec,wizard,messages_newtask}.go`, with tests;
   - `internal/handler/router.go`, the stubs in `internal/handler/wizard.go`, and the draft methods in `internal/handler/fake_svc_12_test.go`;
   - this section.
2. **ui: wizard card and month grid.** Owns:
   - `internal/ui/wizard.go`: it replaces the `WizardCard`, `MonthGrid` and `WizardAsk` stubs. It keeps every other exported name and signature in that file;
   - new files `internal/ui/wizard_card.go`, `internal/ui/wizard_grid.go` and `internal/ui/messages_wizard.go` (keys `ui.wizard.*`);
   - new tests `internal/ui/wizard_card_test.go` and `wizard_grid_test.go`, plus new fixtures under `internal/ui/testdata/wizard_*`.

   It must not change `render.go`, `TodoList` or the existing fixtures. It tests:
   - the grid for Jalali and Gregorian, including a month change across a year boundary, Esfand 1403 (30 days) and 1404 (29 days), past days, the first weekday per calendar and Persian digits;
   - every step's keyboard (Skip, Back and Cancel where the table says), with every callback at most 64 bytes;
   - the summary in en/fa and Gregorian/Jalali.
3. **handler: the wizard flow.** Owns:
   - `internal/handler/wizard.go` (it replaces the stubs), new `internal/handler/wizard_*.go` files and `internal/handler/messages_wizard.go` (keys `wizard.*`);
   - `callbacks.go`: dispatching `ActWiz*` to the wizard;
   - `cmd_todo.go`: bare `/add` starts the wizard, and the `/start` and `/help` replies carry `ui.NewTaskKeyboard` in private chats;
   - `messages_todo.go`: the `/new` line in `todo.help`, in en and fa;
   - `cmd_settings.go`: `/lang <lang>` attaches the keyboard in the new language;
   - `callbacks.go` also: the language buttons reset `KBVersion` to 0 in a private chat (see "The reply keyboard");
   - the keyboard hooks `newTaskKeyboardCtx` and `offerNewTaskKeyboard` in `wizard.go`;
   - `assign.go` (`renderList`): appends a row with `ui.NewTaskButton` under the list. It does not change `ui.TodoList`;
   - `cmd/todo-bot/run.go` (`menu`) and `cmd/todo-bot/messages_main.go` (`cmd.menu.new` in en and fa);
   - the draft methods on the fakes that embed `service.Service`: `fake_svc_11_test.go`, `router_test.go` (`prefsOnly`, `prefsPanics`) and `assign_test.go`. `onWizardText` calls `Svc.Draft` for non-command text in private chats (and for replies in groups), so without them the call panics on the nil embedded interface, `Handle` recovers it, and tests fail confusingly;
   - new handler tests. They use a fake service with an in-memory, version-checked draft map (or the real service on SQLite in `t.TempDir()`), `tgtest.FakeSender` and `clocktest.Fake`. They build button data with `ui.Encode` and assert on the stored draft and the service calls, not on card text, because `WizardCard` may still be a stub.

   It covers Skip, Back, Cancel and Edit from every step; an expired draft; a stale version; another member's press (toast); a non-reply text in a group (ignored); plain text in a private chat on a text step (an answer, not an item) and after Cancel (an item); Nobody and Me in a group and in a private chat; the reply-keyboard label in en and fa; the keyboard attached once and then not again, never in a group, never on an inline-keyboard reply, and again after `/lang`; and the Save mapping above.
4. **e2e + docs.** Owns:
   - a new `internal/e2e/wizard_test.go`. With the fake clock and the mock Telegram server, it checks:
     - every entry point in en and fa;
     - the full path text → Tomorrow → 15:00 → Weekly → @member → Save, against `/add @member text tomorrow 15:00 every week` (see "Equivalence");
     - the reply keyboard on the first private reply, and its version in the database;
     - Jalali monthly and yearly items;
     - a wizard started on node A and finished on node B after A stops (two-node);
     - expiry after 30 minutes;
     - the group rules;
   - `internal/e2e/mocktg_test.go`: recording `reply_markup` (inline and reply keyboards, ForceReply), the callback data of buttons, and the message IDs it returns, so that the test can press buttons and reply to cards;
   - `internal/e2e/harness_test.go`, if the harness needs a helper for this;
   - `docs/USAGE.md`, `README.md` and `README.fa.md`.

   It needs PRs 2 and 3 merged first. Its yearly case needs PR 5.
5. **nlparse: yearly, the remainder of #70.** A separate small PR, independent of PRs 2–4. It depends on #69 (this PR) for `recur.Yearly`. Owns:
   - `internal/nlparse/grammar.go`: `every year`, `yearly`, `هر سال` and `سالانه`;
   - `internal/nlparse/resolve.go`: a `recur.Yearly` case in `buildDatedRule`, and Yearly cases in `buildRule` and `firstOccurrence`;
   - `internal/nlparse/nlparse.go`, if needed;
   - the existing `internal/nlparse/repeat_test.go`.

   It does not change the exported `nlparse` API. `<date> <time> every year` starts on that date at that time and repeats on that month and day, in the user's calendar. A bare `every year` starts on today's date. Existing phrases keep their results.

The v0.2 Postgres and MySQL providers ship their own `0004_wizard.sql` (§9) and must pass the new conformance cases.

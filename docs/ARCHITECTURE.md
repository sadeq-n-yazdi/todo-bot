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
- **store**: the `Store` interface, plus `ErrNotFound`. `WithTx(fn)` hands `fn` a store bound to the transaction, and that store is the only one `fn` may use. Assignment (§11) adds `SetUsername`, `UserByUsername` and `ListAssigned`.
- **tg**:
  - Types: `Message`, `Callback`, `Update`, `Button`, `OutMessage` (HTML text, inline keyboard, ForceReply).
  - `Message.FromUsername` and `Callback.FromUsername` carry the sender's @username without "@", as Telegram sends it (§11).
  - `Sender{Send, Edit, AnswerCallback}`.
  - `APIError{Code, Desc, RetryAfter, MigrateTo}` and `IsPermanent(err)`.
- **ui**:
  - `Action{Kind, ID, Arg}` with `Encode` (at most 64 bytes) and `Decode`.
  - `TodoList`, `ReminderMsg`, `Digest` and `FormatWhen`.
  - `Mention(username)` renders `@name` for HTML, or "" (§11).
- **service**: the `Service` interface, `Actor{UserID, ChatID, ChatKind, ChatTitle}`, and `Waker{Wake()}`. Assignment (§11) adds `NoteUsername`, `UserByUsername`, `AddTodoFor` and `ListAssigned`.
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
- **Schema:** see `migrations/sqlite/0001_init.sql`, `0002_multinode.sql` (§9) and `0003_assign.sql` (§11).
  - `users`: id, tz, lang, calendar and `username` (unique where not null).
  - `chats`: kind, title, `next_num` (per-chat item numbering), and digest settings (`digest_time`, `digest_by`, `digest_next_at`).
  - `todos`: num (per chat), text, status, `due_at`, `recur`, `tz`, `created_by`, `assignee_id`.
  - `reminders`: `todo_id`, `chat_id`, `fire_at`, kind, state, attempts, `message_id`.
  - There is a partial index on pending reminders, and a partial unique index that allows only one pending `due` reminder per todo.

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
- The v0.2 providers must also ship `0003_assign.sql` (§11). A plain unique index on `users.username` is enough on Postgres and MySQL, because both allow several NULLs in a unique index. The index on `todos (assignee_id, status)` is a plain index there.
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
- The item stays on the creating chat's list. List rows of assigned items show `→ @name` (`ui.Mention(t.AssigneeName)`).
- `/list` in a **private chat** also shows a section headed "Assigned to me", built from `Service.ListAssigned(userID)`.
  - That list excludes items in the user's own private chat.
  - The section is not paged and has per-item buttons.
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

### Scheduler routing and the 403 fallback
- `scheduler.Backend` is **unchanged**. For each claimed `DueReminder`:
  1. If `dr.Todo.AssigneeID == 0`, the reminder is delivered as today.
  2. Otherwise the scheduler renders `ui.ReminderMsg` in the **assignee's** prefs (`Prefs(AssigneeID)`) and sends it to chat `AssigneeID`, the assignee's private chat. Recurrence is still computed in `todo.TZ`.
  3. If that send fails **permanently** (`tg.IsPermanent`: 403 blocked or never started, chat not found), the scheduler falls back **once**. It renders the reminder in the creator's prefs, prefixes it with `ui.Mention(dr.Todo.AssigneeName)`, and sends it to `dr.ChatID`, the creating chat, through the usual `sendWithRetry` (migration and 429 handling).
     - If the fallback succeeds, the scheduler calls `ReminderSent(id, fallbackMsgID)`.
     - If the fallback fails, it calls `ReminderFailed` with that error's permanence.
  4. A **transient** error on the DM goes through the normal backoff (`ReminderFailed(permanent=false)`) and does not fall back.
- The fallback happens inside one claim, within the same budget (`end`). The scheduler keeps no "already fell back" state. Each new reminder, including the next occurrence of a recurring item, tries the DM first, so a user who starts the bot later begins to receive reminders by DM.

### Multi-node
- Usernames, assignees and the authorisation inputs live only in the database. Any node handles any update. The rule from §9 stays: coordinate only through claims, the lease, `MarkUpdate` and `TakePrompt`.
- `SetUsername` is one transaction, and the unique index guarantees one owner per name.
  - Concurrent calls that give one name to different users must end with exactly one owner and **no error**. The conformance case `UsernameConcurrent` checks this.
  - SQLite serialises them (`_txlock=immediate`).
  - The Postgres and MySQL providers (v0.2) must retry on a unique violation.
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
   - `internal/ui/mention.go`;
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

# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- A date and time combined with a repeat (#70): `call mom tomorrow 15:00 every week` and `گزارش فردا ساعت ۱۵ هر هفته` start on that date and repeat on that weekday at that time. Monthly repeats use the day of the month, and a Jalali month name (`۱ آبان هر ماه`) anchors a Jalali monthly repeat. The first due time is always the rule's next occurrence at or after now.
- Yearly repeats (#70): `every year`, `yearly`, `هر سال` and `سالانه`. With a date, such as `birthday 15 Oct 9am every year` or `تولد ۱ آبان هر سال`, the item repeats on that month and day in the calendar of the date. A bare `every year` starts today. A date of 29 Feb or 30 Esfand starts on the next real occurrence and falls on the last day of the month in common years.

## [0.1.1] - 2026-10-10

### Added
- A date at the start of the text is recognised (#65): `tomorrow 9am call mom`, `امروز ساعت ۱۵ بچه را از مدرسه بردار`.
  - A bare hour from 1 to 12 at the start (`at 5 …`, `ساعت ۲ …`) needs am/pm, a colon or a part of day, so counts like `at 9 lives` stay plain text.
  - Part-of-day words after a date: morning, noon, afternoon, evening, night; صبح، ظهر، بعدازظهر، عصر، شب. For example, `فردا صبح نان بخر` is due tomorrow at 09:00.
- Assign a task to another member (#58): `/add @username …`.
  - The reminder goes to the assignee's private chat, in their language and time zone, with Done and snooze buttons. If that message is refused, for example because they blocked the bot, it goes once to the chat where the task was added, with a mention.
  - The task stays in the list where it was added, showing `→ @name`. The assignee's private `/list` shows it under "Assigned to me".
  - The creator or the assignee can complete it. When the assignee does, the chat where it was added gets a "did" notice.
  - The bot learns usernames as people use it, so the assignee must have started the bot. A username that moves to another account is cleared from the old one.
  - Schema migration 0003 stores usernames and the assignee.

### Fixed
- A date is now found when the text also has an `@mention` or `#tag` after it or inside it, for example `pick up our son today 15:00 @wife` (#57). The mention is kept in the item's text.

## [0.1.0] - 2026-10-10

### Added
- Release workflow (#16): a `v*` tag builds static linux amd64 and arm64 binaries and publishes them, with `SHA256SUMS`, as a GitHub release.
- End-to-end tests (#14), run on the real stack with a fake clock. They cover:
  - snooze and Done, the DST change, Farsi/Jalali monthly items, and catch-up after downtime;
  - digests, group chats and restart;
  - two nodes sharing one database, with exactly-once delivery and failover after both a clean stop and a crash.
- Inline buttons and settings (#12):
  - list buttons (✅ ✏️ ⏰ 🗑) with paging, and reminder buttons (Done, 10m, 1h, Tomorrow);
  - reply prompts for edit and remind;
  - `/remind`, `/tz`, `/digest`, `/lang` and `/calendar`.
- User docs (#15):
  - README in English and Farsi, with a quick start, the full config table and the BotFather setup;
  - `docs/USAGE.md` with a reference for every command, with English and Farsi examples.
- `todo-bot` binary wiring (#13):
  - subcommands: `run`, `migrate`, `backup` and `version`;
  - leader-gated polling, with the bot commands set in English and Farsi;
  - the scheduler runs on every node, and startup retries reaching Telegram with backoff;
  - shutdown is bounded, and secrets are kept out of logs.
- Leader lease loop (#27). It decides which node polls Telegram:
  - the leader renews its lease every ttl/3, and steps down by ttl/2 after its last successful renewal, even if a database call hangs;
  - only one renewal call is in flight at a time, and the lease is released on shutdown.
- Service layer (#8):
  - todos, reminders, recurrence in the user's zone and calendar (DST- and Jalali-safe), snooze, digests and preferences;
  - every write after a claim is conditional on the claim token;
  - edits and preference changes are done inside a single transaction.
- A storage layer that is ready for several nodes (#25, #26):
  - `sqlstore` with per-database dialects, and SQLite ported onto it;
  - atomic claims for reminders and digests, a leader lease, update dedupe and persisted prompts;
  - versioned migrations, kept in step with SQLite's `user_version`;
  - a conformance suite that every database provider must pass.
- Reminder and digest scheduler (#10). It is safe to run on several nodes:
  - it claims due reminders before sending them;
  - it waits out Telegram rate limits only while its claim is still valid;
  - it records sends during shutdown and isolates crashes to one reminder;
  - it retries a digest that fails within the hour.
- Todo commands in English and Farsi (#11):
  - /start, /help, /add (and plain text in a private chat), /list, /done, /del, /edit and /today;
  - item text is capped at 1000 characters;
  - confirmations stay safely under Telegram's message limit.
- Telegram adapter (#6):
  - long polling that loses no updates on a lease handover;
  - HTML messages with inline keyboards;
  - error mapping (429 retry-after, chat migration, permanent failures);
  - token redaction in errors;
  - size limits checked before any request is sent.
- Config loading from the environment (#2):
  - every problem is reported at once;
  - secrets are never echoed in errors;
  - SQLite URLs are built safely;
  - webhook-mode settings are validated.
- Multi-node contracts (#24):
  - a store provider registry, opened by URL scheme;
  - claim-based scheduling with conditional writes;
  - the leader lease;
  - persisted prompts;
  - update dedupe;
  - per-dialect migrations.
- Deployment tooling (#9):
  - a hardened systemd unit (exposure score 1.1), with sysusers and an env template;
  - `scripts/deploy.sh` with bootstrap, update, rollback, status and logs.
- On update, `deploy.sh` backs up the database, keeps the last healthy binary, health-checks the bot against restart loops, and prunes backups.
- App and bot icon, social preview and export script (#17).
- Natural-language dates in English and Farsi (#5):
  - relative times, weekdays and ordinals;
  - Jalali month names;
  - recurrence phrases;
  - Persian digits.
- SQLite store with embedded migrations, WAL mode and online backup (#4).
- Localized UI rendering in English and Farsi, for Gregorian and Jalali dates:
  - todo lists with buttons and paging;
  - reminders with snooze buttons;
  - the daily digest.

  Callback data is limited to 64 bytes and fuzz-tested (#7).
- Project skeleton with frozen contracts, CI and an MIT license (#1).
- Jalali↔Gregorian calendar conversion (`internal/calendar`) (#1).
- i18n core for English and Farsi: catalog, Persian digits, month and weekday names (#1).
- Handler router with an allowlist, `/whoami` and panic recovery (#1).
- Recurrence engine with daily, weekly, monthly (Gregorian or Jalali) and every-N-days rules (#3).
  - Times follow wall-clock time across DST changes.
  - A time that falls in the spring-forward gap fires at the shifted time, never earlier.

[Unreleased]: https://github.com/sadeq-n-yazdi/todo-bot/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/sadeq-n-yazdi/todo-bot/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/sadeq-n-yazdi/todo-bot/releases/tag/v0.1.0

# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
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

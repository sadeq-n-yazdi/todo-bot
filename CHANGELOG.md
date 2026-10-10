# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
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

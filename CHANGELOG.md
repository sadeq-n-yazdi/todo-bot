# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Project skeleton with frozen contracts, CI and an MIT license (#1).
- Jalali↔Gregorian calendar conversion (`internal/calendar`) (#1).
- i18n core for English and Farsi: catalog, Persian digits, month and weekday names (#1).
- Handler router with an allowlist, `/whoami` and panic recovery (#1).
- Recurrence engine with daily, weekly, monthly (Gregorian or Jalali) and every-N-days rules (#3).
  - Times follow wall-clock time across DST changes.
  - A time that falls in the spring-forward gap fires at the shifted time, never earlier.

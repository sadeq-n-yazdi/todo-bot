# Progress

This page is updated on `main` after every merged PR.

Milestone: [v0.1.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/1)

| # | Wave | Issue | Status | PR |
|---|---|---|---|---|
| 1 | 0 | Bootstrap and contracts (+ calendar, i18n core) | ✅ merged | #18 |
| 2 | 1 | Config and logging | ⏳ todo | |
| 3 | 1 | Recurrence engine (DST, Jalali monthly) | 🔨 in progress | |
| 4 | 1 | SQLite store and migrations | 🔨 in progress | |
| 5 | 1 | Natural-language parser (en + fa, Jalali) | 🔨 in progress | |
| 6 | 2 | Telegram adapter | ⏳ todo | |
| 7 | 2 | UI rendering and callback codec | 🔨 in progress | |
| 8 | 2 | Service layer | ⏳ todo | |
| 9 | 2 | Deployment tooling | ⏳ todo | |
| 17 | 2 | App and bot icon | ⏳ todo | |
| 10 | 3 | Scheduler | ⏳ todo | |
| 11 | 3 | Handlers: todo commands | ⏳ todo | |
| 12 | 3 | Handlers: callbacks, prompts, settings | ⏳ todo | |
| 13 | 4 | Main wiring | ⏳ todo | |
| 14 | 4 | End-to-end tests | ⏳ todo | |
| 15 | 4 | User documentation (en + fa) | ⏳ todo | |
| 16 | 5 | Release v0.1.0 and deploy to vps00 | ⏳ todo | |

## Log
- **2026-10-10**
  - Planned the project and created the repo and 17 issues.
  - Added the requirement for English/Farsi and Gregorian/Jalali support.
  - Implemented the calendar and i18n cores in #1, so that later waves can build on them.
- 2026-10-10: #1 merged (#18). Wave 1 started in critical-path order: #3, #4, #5 and #7 (UI pulled forward).

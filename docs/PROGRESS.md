# Progress

This page is updated on `main` after every merged PR.

Milestones: [v0.1.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/1) (single host, multi-node seams) · [v0.2.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/2) (Postgres/MySQL, webhook, two-node HA)

| # | Wave | Issue | Status | PR |
|---|---|---|---|---|
| 1 | 0 | Bootstrap and contracts (+ calendar, i18n core) | ✅ merged | #18 |
| 2 | 1 | Config and logging | ⏳ todo | |
| 3 | 1 | Recurrence engine (DST, Jalali monthly) | ✅ merged | #19 |
| 24 | 1.5 | Contracts v2: registry, multi-node store methods | 🔍 in review | #38 |
| 4 | 1 | SQLite store and migrations | ✅ merged | #21 |
| 5 | 1 | Natural-language parser (en + fa, Jalali) | ✅ merged | #22 |
| 25 | 2 | sqlstore + Dialect; port SQLite; multi-node methods | ⏳ todo | |
| 26 | 2 | storetest conformance suite | ⏳ todo | |
| 27 | 2 | Leader lease loop | ⏳ todo | |
| 6 | 2 | Telegram adapter | ⏳ todo | |
| 7 | 2 | UI rendering and callback codec | ✅ merged | #20 |
| 8 | 2 | Service layer | ⏳ todo | |
| 9 | 2 | Deployment tooling | ✅ merged | #37 |
| 17 | 2 | App and bot icon | ✅ merged | #23 |
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
- **2026-10-10**
  - #3 merged (#19).
  - New owner requirements:
    - pluggable databases (SQLite, PostgreSQL, MySQL);
    - multiple VPSs, active-active with failover.
  - The seams ship in v0.1; the HA work ships in v0.2. The design is being reworked.
- **2026-10-10** #7 merged (#20). #17 (icon) started.
- **2026-10-10**
  - Merged #4 (#21), #5 (#22), #17 (#23) and #9 (#37).
  - The multi-node design is done. ARCHITECTURE §9 arrives with #38.
  - New issues:
    - #24–#27 for v0.1: contracts v2, sqlstore/Dialect, the conformance suite and the lease;
    - #28–#36 for v0.2 under the new milestone.
  - Scope comments were added to #2, #6, #8, #9, #10, #12, #13, #14 and #16.
  - Security fixes before merge:
    - the deploy script no longer lets root follow paths the service user controls;
    - store URL parse errors no longer echo credentials.

# Progress

This page is updated on `main` after every merged PR.

Milestones: [v0.1.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/1) (single host, multi-node seams, **released**) · [v0.1.1](https://github.com/sadeq-n-yazdi/todo-bot/milestone/3) (assign tasks to members, **released**) · [v0.2.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/2) (Postgres/MySQL, webhook, two-node HA)

| # | Wave | Issue | Status | PR |
|---|---|---|---|---|
| 1 | 0 | Bootstrap and contracts (+ calendar, i18n core) | ✅ merged | #18 |
| 2 | 1 | Config and logging | ✅ merged | #39 |
| 3 | 1 | Recurrence engine (DST, Jalali monthly) | ✅ merged | #19 |
| 24 | 1.5 | Contracts v2: registry, multi-node store methods | ✅ merged | #38 |
| 4 | 1 | SQLite store and migrations | ✅ merged | #21 |
| 5 | 1 | Natural-language parser (en + fa, Jalali) | ✅ merged | #22 |
| 25 | 2 | sqlstore + Dialect; port SQLite; multi-node methods | ✅ merged | #42 |
| 26 | 2 | storetest conformance suite | ✅ merged | #42 |
| 27 | 2 | Leader lease loop | ✅ merged | #47 |
| 6 | 2 | Telegram adapter | ✅ merged | #40 |
| 7 | 2 | UI rendering and callback codec | ✅ merged | #20 |
| 8 | 2 | Service layer | ✅ merged | #48 |
| 9 | 2 | Deployment tooling | ✅ merged | #37 |
| 17 | 2 | App and bot icon | ✅ merged | #23 |
| 10 | 3 | Scheduler | ✅ merged | #41 |
| 11 | 3 | Handlers: todo commands | ✅ merged | #43 |
| 12 | 3 | Handlers: callbacks, prompts, settings | ✅ merged | #44 |
| 13 | 4 | Main wiring | ✅ merged | #52 |
| 14 | 4 | End-to-end tests | ✅ merged | #56 |
| 15 | 4 | User documentation (en + fa) | ✅ merged | #55 |
| 16 | 5 | Release v0.1.0 and deploy to vps00 | ✅ released and deployed | #59 |

### v0.1.1

| # | Issue | Status | PR |
|---|---|---|---|
| 57 | nlparse: a trailing @mention breaks date parsing | ✅ merged | #60 |
| 58 | Assign a task to another member (reminder by DM) | ✅ merged | #61, #63, #64, #62, #67 |
| 65 | nlparse: recognise a date phrase at the start of the text | ✅ merged | #66 |

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
- **2026-10-10**
  - #24 merged (#38) after an architecture review. The review found that a node whose claim had expired could resurrect a sent reminder; that is fixed with token-conditional writes.
  - Wave 2 started: #25+#26 (one PR), #6 and #2.
  - #8 (service) and #27 (lease) start after #25.
- **2026-10-10**
  - v0.1.0 released and deployed to vps00. Rollback was exercised (update, rollback, update).
  - v0.1.1 started: #57 merged (#60) after review. The review caught `@3pm` being hidden as a mention, so a mention now has to start with a letter or underscore.
  - #58 part 1 (contracts) is in progress.
  - #58 part 1 (contracts) merged (#61) after an architecture review. The review moved `ui.Mention` to link by user ID, so a username that moved to another account can't ping the wrong person. Parts 2 (service), 3 (handler and ui) and 4 (scheduler and docs) started in parallel.
  - #58 parts 2–4 merged: the service (#63), the handler and ui (#64), and the scheduler and docs (#62). Each was reviewed first. The reviews led to:
    - the assignee being allowed to act only from their own private chat or the item's chat;
    - the Farsi docs example being reordered so its date parses;
    - the mention rule matching Telegram usernames (5–32 characters, starting with a letter).
  - Filed #65, after a review showed that a date at the start of the text is ignored. The e2e tests for #58 and #65 started.
  - #58 e2e tests merged (#67), closing #58. They found no bugs.
  - #65 merged (#66). The review compared old and new results over about 300 inputs and found false positives from a bare hour at the start (`at 9 lives`), which are fixed. Part-of-day words were added.
  - v0.1.1 tagged.
- **2026-10-11**
  - v0.1.1 deployed to vps00. Migration 0003 was applied at startup and the database was backed up first. The service is active with 0 restarts and about 19MB of memory.

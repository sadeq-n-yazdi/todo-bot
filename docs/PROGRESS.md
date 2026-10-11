# Progress

This page is updated on `main` after every merged PR.

Milestones: [v0.1.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/1) (single host, multi-node seams, **released**) · [v0.1.1](https://github.com/sadeq-n-yazdi/todo-bot/milestone/3) (assign tasks to members, **released**) · v0.1.2 (step-by-step wizard, **released**) · v0.1.3 (retry policy, **released and deployed**) · [v0.1.4](https://github.com/sadeq-n-yazdi/todo-bot/milestone/5) (hardening, **released**) · [v0.2.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/2) (Postgres/MySQL, webhook, two-node HA)

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

### v0.1.2

| # | Issue | Status | PR |
|---|---|---|---|
| 68 | Step-by-step task creation (wizard) | ✅ done | #69, #73, #74, #75, #76 |
| 70 | nlparse: combine a date/time with every week, month or year | ✅ done | #71, #72 |

### v0.1.4

| # | Issue | Status | PR |
|---|---|---|---|
| 45 | Language and calendar buttons through the ui codec | ✅ merged | #78 |
| 46 | Bound each scheduler send by the claim deadline | ✅ merged | #80 |
| 49 | Keep the digest claim when MigrateChat merges into an existing chat | ✅ merged | #79 |
| 50 | Never move a lease's held_until backwards | ✅ merged | #79 |
| 51 | Lease loop: test and doc follow-ups | ✅ merged | #81 |
| 53 | Open the store without migrations for backup | ✅ merged | #81 |
| 54 | Startup getMe retry: honour RetryAfter | ✅ merged | #81 |
| 82 | Minimum claim TTL; tighten the digest hang test | ✅ merged | #83 |
| 84 | Backup and startup follow-ups from the #81 review | 📋 open | |

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
  - Planned v0.1.2: the step-by-step wizard (#68), filed after the owner decided where it starts, which steps it has, the date grid and group rules. Contracts PR #69 is open.
  - #70 weekly and monthly merged (#71). The review found first due times in the past, a first due that wasn't a real occurrence of the rule, and dates jumping a year; all three are fixed. A property check over every combined test row now guards this.
  - #68 contracts merged (#69). The review found that Save could lose the item, so Save now deletes the draft and adds the item in one transaction. It also found that the reply keyboard could be attached to a message that is edited later; the wizard card now always has an inline keyboard. The ui, handler and yearly-parser PRs started in parallel.
  - #70 yearly merged (#72), closing #70. The review compared old and new results over about 8,000 inputs and found no false positives (`happy new year card` and `سال نو` stay plain text).
  - #68 ui merged (#73): the wizard card and the month grid. The review found a blank button label that Telegram rejects, no cap on member buttons (Telegram allows 100), and a card that could pass 4096 characters; all three are fixed.
  - #68 flow merged (#74). The review found that a second press of Save or Cancel replaced the finished card with "expired", and that editing a date skipped the repeat step, so a weekly item could silently become a one-off. Both are fixed. The e2e tests and user docs started.
  - #68 e2e tests and docs merged (#76), closing #68. The review found four doc claims that did not match the code; all are fixed. A second press on a closed card now shows a neutral toast (#75).
  - v0.1.2 tagged.
  - The owner wrote the retry policy for reminders (#77): 5 s, 30 s, 2 min, 10 min, 30 min, then give up. The window is short because a retried reminder is not marked late. v0.1.3 tagged.
  - v0.1.3 deployed to vps00 (it includes v0.1.2). Migration 0004 was applied at startup and the database was backed up first. The service is active with 0 restarts and about 24MB of memory. v0.1.1 is kept as `todo-bot.prev` for rollback.
  - Planned v0.1.4: the seven open follow-ups from earlier reviews (#45, #46, #49, #50, #51, #53, #54). Four PRs started in parallel.
  - #45 merged (#78). #46 merged (#80): each send now ends before its claim expires. #49 and #50 merged (#79): a stale lease renewal can no longer move the expiry back, and a chat merge keeps the digest claim. The reviews filed #82.
  - #82 merged (#83). #51, #53 and #54 merged (#81). Its review found that a 502 HTML page from a gateway made startup fatal, which would stop the unit after five restarts; this is fixed and tested. The review also ran the new backup against a database under heavy writes. Filed #84 for the remaining notes. v0.1.4 tagged.

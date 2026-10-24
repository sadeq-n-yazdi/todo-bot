# Progress

This page is updated on `main` after every merged PR.

Milestones: [v0.1.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/1) (single host, multi-node seams, **released**) · [v0.1.1](https://github.com/sadeq-n-yazdi/todo-bot/milestone/3) (assign tasks to members, **released**) · v0.1.2 (step-by-step wizard, **released**) · v0.1.3 (retry policy, **released**) · [v0.1.4](https://github.com/sadeq-n-yazdi/todo-bot/milestone/5) (hardening, **released**) · [v0.1.5](https://github.com/sadeq-n-yazdi/todo-bot/milestone/6) (Nth and last weekday of the month, **released and deployed**) · [v0.1.6](https://github.com/sadeq-n-yazdi/todo-bot/milestone/7) (repeat rule text, deploy hardening, **released and deployed**) · [v0.1.7](https://github.com/sadeq-n-yazdi/todo-bot/milestone/8) (one-off Nth weekday, cleanups, **released**) · [v0.1.8](https://github.com/sadeq-n-yazdi/todo-bot/milestone/9) (edit with the wizard, **released**) · [v0.1.9](https://github.com/sadeq-n-yazdi/todo-bot/milestone/11) (edit follow-ups) · [v0.3.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/10) (web UI) · [v0.2.0](https://github.com/sadeq-n-yazdi/todo-bot/milestone/2) (Postgres/MySQL, webhook, two-node HA)

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
| 84 | Backup and startup follow-ups from the #81 review | ➡️ moved to v0.1.6 | |

### v0.1.5

| # | Issue | Status | PR |
|---|---|---|---|
| 85 | recur: Nth and last weekday of the month | ✅ merged | #88 |
| 86 | nlparse: every last/Nth weekday of the month (en + fa) | ✅ merged | #90 |
| 87 | Wizard: monthly on the Nth/last weekday; docs and e2e | ✅ merged | #89 |

### v0.1.6

| # | Issue | Status | PR |
|---|---|---|---|
| 91 | Show the repeat rule next to 🔁 | ✅ merged | #92 |
| 84 | Backup and startup follow-ups from the #81 review | ✅ merged | #93 |
| 94 | Flaky test: TestPollerAdvancesOffsetAndSurvivesHandlerError | ✅ merged | #95 |

### v0.1.7

| # | Issue | Status | PR |
|---|---|---|---|
| 96 | Cleanups: legacy settings data, repeat after a calendar change, test guard | ✅ merged | #99 |
| 97 | nlparse: one-off Nth or last weekday of this or next month | ✅ merged | #100, #101 |

### v0.1.8

| # | Issue | Status | PR |
|---|---|---|---|
| 103 | Edit: contracts (model, service, ui) | ✅ merged | #107, #108 |
| 104 | Edit: service (`UpdateTodo`, `SaveEditDraft`) | ✅ merged | #111 |
| 105 | Edit: the wizard card in edit mode | ✅ merged | #110 |
| 106 | Edit: handler, DM to a new assignee, e2e | ✅ merged | #112 |

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
  - v0.1.4 deployed to vps00. It has no schema change. The database was backed up first. The service is active with 0 restarts and about 20MB of memory. v0.1.3 is kept as `todo-bot.prev` for rollback.
  - The owner asked for "every Friday of the last week of the month at 4pm". Planned v0.1.5 (#85–#87). The owner decided: the last such weekday inside the month, also the 1st to 4th, in the user's calendar, from text and from the wizard.
  - #85 merged (#88). The review checked every rule against listed month dates for 2016–2034 and Jalali 1394–1414 in six zones, and ran a fuzz test. It asked for a Tehran DST test.
  - #86 merged (#90). The review compared old and new results over about 1,000 inputs. Every changed result was the new rule, and each had a repeat word and a month word. It found that "of the last week of the month" was not read; this is fixed.
  - #87 merged (#89). The review found that the docs and the PR text did not match the code; these are fixed. A date edit never saves a choice that does not fit. v0.1.5 tagged.
  - v0.1.5 deployed to vps00. It has no schema change. The database was backed up first. v0.1.4 is kept as `todo-bot.prev` for rollback.
- **2026-10-12**
  - Planned v0.1.6: the repeat rule as text (#91) and the open follow-ups of #84.
  - #91 merged (#92). The review found that a long rule on every row could push page 1 of `/list` past 4096 characters. The rule text is now clipped, and the "Assigned to me" rows show only 🔁. In Farsi, days start on Saturday, and Monday to Friday is not called "workdays".
  - #92 was merged while its CI showed a failure. The failure was a flaky poller test in a package that #92 did not change, and main CI passed. Filed #94. Merges now wait on the exit status of the check itself.
  - #84 merged (#93). The review found that a database under a folder the service user cannot open still read as "no database yet". This is fixed and tested. The deploy harness now runs in CI, and shellcheck on the runner found ten warnings in it, all fixed.
  - #94 merged (#95). The fake Telegram server in the test did not wait like a real long poll. Both the author and the review reproduced the CI failure with a short delay. v0.1.6 tagged.
  - v0.1.6 deployed to vps00 with the new deploy.sh. It has no schema change. The database was backed up first. v0.1.5 is kept as `todo-bot.prev` for rollback.
- **2026-10-13**
  - #96 merged (#99). The review found that a keyboard test sent old button data, so it passed for the wrong reason. The fix makes the test fail when the group path is broken. The calendar check now runs before the repeat-fit check.
  - #97: the review found that other repeat words, such as "every week" or "هفتگی", gave a one-off with no repeat. It also asked that a phrase with no time, typed on the day itself, keep today. Both are fixed, and a second review compared the results with main.
  - Owner decisions for #98 (edit with the wizard): `/edit N` with no text opens the wizard, a schedule change keeps the custom reminders, and a new assignee gets a short DM.
- **2026-10-18**
  - The owner merged #99 and #100. #100 went in before its review fix.
- **2026-10-24**
  - The review fix of #100 merged as #101. v0.1.7 tagged.
  - Posted the #98 design on the issue.
  - The owner asked for a web UI: a Telegram Mini App and an installable web app at todo.sadeq.uk. A Caddy proxy on vps03 serves all sadeq.uk sites on port 443 with DNS-01 certificates. todo-bot moves from vps00 to vps03. Filed #102 for the design.
  - Merged the #98 parts: contracts #107 and #108, card #110, service #111 and handler #112. The review of #112 found that a date-only item got a 23:59 reminder after a schedule edit. The fix went in before the merge. v0.1.8 tagged.
  - The review of #111 after the merge approved it. Its follow-ups are in #114.
  - The owner accepted D1–D12 of the web design. The review fixes of the design are in #113.

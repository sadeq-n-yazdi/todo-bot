# Working on todo-bot

A Go Telegram todo bot with reminders. It supports English/Farsi and Gregorian/Jalali.

Read `docs/ARCHITECTURE.md` before changing anything.

## Rules for every change

1. **Stay inside your issue's files.**
   - Each GitHub issue lists the files and packages it owns.
   - Do not edit other packages. If you need something from another package, write it in the PR description.
2. **The contracts are frozen.** These are:
   - the exported types and signatures in `internal/{model,store,tg,ui,service,scheduler,recur,nlparse,config,i18n,calendar}`
   - `internal/handler/router.go`
   - `migrations/0001_init.sql`

   Change them only when unavoidable. If you do, add a section headed `CONTRACT CHANGE` to the PR description explaining why.
3. **Do not run `go get`, and do not edit `go.mod` or `go.sum`.** The dependencies are pinned. If you need a new module, ask in the PR.
4. **Do not edit `CHANGELOG.md` or `docs/PROGRESS.md`.** The maintainer updates them on `main` after each merge.
5. **Work test-first.**
   - Use `clocktest.Fake` for anything involving time.
   - Use `tgtest.FakeSender` for Telegram.
   - Use a real SQLite database in `t.TempDir()` for storage.
6. **`make ci` must pass before you open a PR.** It runs vet, golangci-lint v2.14.0 (built locally into `./bin`), `test -race` and a static linux build.
7. **Branch and PR conventions.**
   - Name the branch `issue-<N>-<slug>`.
   - Put `Closes #N` in the PR body, and copy the issue's acceptance criteria into the PR as a checklist.
   - Write commit messages in the imperative, for example "Add recurrence engine".

## Internationalisation

- **Translations.**
  - Never hard-code user-facing text. Register every string in English **and** Farsi with `i18n.Register`.
  - Do this from an `init()` in a `messages*.go` file owned by your package or issue.
  - Duplicate keys panic, so prefix your keys: `ui.`, `todo.`, `settings.`, and so on.
- **Digits.**
  - Show numbers and dates with `Locale.Num` / `Locale.Digits`, which give Persian digits in Farsi.
  - Keep command examples (`/done 3`) in ASCII.
- **User input.** Pass all user input through `i18n.NormalizeDigits` before parsing it.
- **Calendars.**
  - Dates are shown in the user's calendar (`internal/calendar`).
  - Recurrence math is done on calendar dates with `time.Date`, never by adding `24*time.Hour`.
- **Farsi typography.** Write the zero-width non-joiner (ZWNJ) in Farsi strings as `‌` (for example `"سه‌شنبه"`), because staticcheck ST1018 flags a literal one.

## Commands

```
make ci            # everything CI runs
make test          # go test -race ./...
make lint
make build-linux   # dist/todo-bot-linux-amd64 (ARCH=arm64 for arm)
```

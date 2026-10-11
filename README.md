<p align="center">
  <img src="assets/icon.svg" width="128" height="128" alt="todo-bot icon: a checklist with an amber bell">
</p>

# todo-bot

A self-hosted Telegram todo bot with reminders, for you and your family.

English | [فارسی](README.fa.md)

You write an item the way you would say it, for example `buy milk tomorrow 18:00` or `pills every day 9am`. The bot keeps the list, reminds you at the right time, and sends a daily digest of what is due. It speaks English and Farsi, and shows dates in the Gregorian or the Jalali (Persian) calendar.

## Features

- **Todo lists.** A private list in your own chat with the bot, and a separate shared list in each group.
- **Plain text adds an item.** In a private chat, any message that is not a command is added to your list.
- **Step-by-step wizard.** Do not want to write the date in a sentence? Send `/new`, send `/add` with no text, press **➕ New task** under `/list`, or press the **➕ New task** button of the reply keyboard in a private chat. The bot then asks for the text, the date (quick picks or a month grid in your calendar), the time, the repeat, the assignee and a final check. **Cancel** is on every step. Most steps also have **Back**, and the optional steps have **Skip**. The last step has **Save** and **Edit**. In a group, answer the text questions with Telegram's **Reply**. Only the person who started the wizard can press its buttons. A draft expires after 30 minutes with no answer. See [Usage](docs/USAGE.md#step-by-step-task-new).
- **Natural-language dates.** `tomorrow 18:00`, `in 2h`, `Fri 14:30`, `15 Oct`, `25 Mehr`. English and Farsi phrases are both understood, whatever language you chose for the bot.
- **Reminders.** A due time becomes a reminder. When it fires you get buttons to mark the item done or to snooze it.
- **Recurring items.** Daily, weekdays, weekly on chosen days, monthly, yearly (`every year`) and every N days.
- **Assign to someone.** `/add @sara_home pick up our son today 15:00` keeps the item in the chat where you typed it, but sends its reminders to the assignee's private chat. The assignee needs a Telegram @username, must press Start once and be in `TODO_ALLOWED_USERS`. People who used the bot before this release become known by username after their next message. Assigned items show in the assignee's `/list` under "Assigned to me", either of you can complete them (when the assignee does, the chat where the item was added gets a "did" notice), and if the assignee cannot be messaged, the reminder falls back once to the chat where the item was added. See [Usage](docs/USAGE.md#assigning-an-item-to-someone-add-username-text-when).
- **Snooze.** 10 minutes, 1 hour, or tomorrow, from the reminder message.
- **Daily digest.** Overdue and today's items at a time you choose, per chat. `/today` shows it on demand.
- **English and Farsi (فارسی).** Per-user language. Farsi replies use Persian digits, and Persian digits are accepted as input.
- **Gregorian and Jalali (هجری شمسی).** Per-user calendar for showing and parsing dates.
- **Private by default.** Only the Telegram user IDs in `TODO_ALLOWED_USERS` can use the bot.
- **Multi-node-ready.** Reminders, the update poller and pending prompts are coordinated through the database, not through memory. See [Architecture](docs/ARCHITECTURE.md) section 9. Version 0.1 runs on one host with SQLite.
- **Small.** One static Go binary, run as a hardened systemd service with a soft Go memory limit of 40 MiB (`GOMEMLIMIT`) and a hard cap of 96M (`MemoryMax`).

> Under construction. Progress: [docs/PROGRESS.md](docs/PROGRESS.md).

## Quick start

You need a Linux host with systemd (a small VPS is enough), Go to build the binary, and a Telegram account.

1. **Create the bot.** Open [@BotFather](https://t.me/BotFather), send `/newbot` and follow the questions. BotFather gives you a token that looks like `123456:ABC-...`. Keep it secret. Then finish the [BotFather setup](#botfather-setup) below.
2. **Get your user ID.** The bot will not start without `TODO_ALLOWED_USERS`, so you need your numeric Telegram ID first. Either:
   - message a third-party ID bot such as @userinfobot, which replies with your ID; or
   - install with a placeholder (`TODO_ALLOWED_USERS=1`), message your new bot, and send `/whoami`. It answers people who are not on the allowlist, and only this command. Then replace the placeholder with the ID it gives you and restart the service.

   Once the bot is running, ask each family member to send it `/whoami` and add their IDs the same way.
3. **Build.** `make build-linux` writes `dist/todo-bot-linux-amd64`. Use `make build-linux ARCH=arm64` for an ARM host.
4. **Install.** `scripts/deploy.sh` runs on **your own machine** and installs over ssh/scp. Its defaults (`HOST`, `PORT`) point at the maintainer's server, so always set your own:

   ```
   HOST=root@your.server PORT=22 scripts/deploy.sh bootstrap
   ```

   This builds the binary, copies it and the systemd unit, creates the `todo-bot` user, and installs the template [`deploy/env.example`](deploy/env.example) as `/etc/todo-bot/env` (owner `root`, mode `0600`) if that file is missing. Until the placeholder token is replaced it does not start the service.
5. **Configure.** On the server, edit `/etc/todo-bot/env` and set at least:

   ```
   TELEGRAM_TOKEN=<token from BotFather>
   TODO_ALLOWED_USERS=11111,22222
   ```

   `TODO_ALLOWED_USERS` is a comma-separated list of the IDs from step 2. The other settings are in [Configuration](#configuration).
6. **Start.** Run the same `bootstrap` command again (or `systemctl enable --now todo-bot` on the server), then `scripts/deploy.sh status` and `logs`. [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) covers updates, rollback and the first-start smoke test.
7. **Say hello.** Send `/start` to the bot. Then read [docs/USAGE.md](docs/USAGE.md).

> **Warning: one token, one process.** Telegram allows only one process to poll for updates per bot token. If you run the bot on your laptop with the production token, both processes fail with `409 Conflict` and the production bot stops receiving messages. Create a **second bot** with `/newbot` for local development and use its token there.

## BotFather setup

Send these to [@BotFather](https://t.me/BotFather), choosing your bot each time.

| Step | What to do |
| --- | --- |
| `/newbot` | Create the bot and get the token. |
| `/setuserpic` | Upload [`assets/icon-640.png`](assets/icon-640.png) as the bot's picture. |
| `/setdescription` | Text shown when someone opens the bot for the first time. Example: `Todo list and reminders for the family. Send /whoami to get your ID.` |
| `/setcommands` | The command menu. Paste the list below. |
| `/setjoingroups` | `Enable` if you want to add the bot to groups, `Disable` to keep it private-chat only. |
| `/setprivacy` | Leave privacy mode **enabled** (the default). The bot only needs commands and replies to its own messages, and both are delivered with privacy on. |

The bot also registers its menu by itself at startup, in English and in Farsi, so `/setcommands` is optional. If you set it by hand, use exactly this text:

```
help - Show help
add - Add an item
list - Show open items
done - Mark an item done
del - Delete an item
edit - Change an item's text
today - Overdue and today's items
remind - Add a reminder to an item
tz - Set your time zone
digest - Set the daily digest
lang - Choose the language
calendar - Choose the calendar
```

`/start` and `/whoami` work but are not in the menu.

## Configuration

The bot reads environment variables, normally from `/etc/todo-bot/env` (lines of `KEY=value`, no quotes, no `export`). See [`deploy/env.example`](deploy/env.example). A mistake in any value is reported at startup, and the bot does not start.

| Variable | Required | Default | Meaning |
| --- | --- | --- | --- |
| `TELEGRAM_TOKEN` | yes | | Bot token from @BotFather. Never commit it or paste it in a chat. |
| `TODO_ALLOWED_USERS` | yes | | Comma-separated numeric Telegram user IDs that may use the bot. Everyone else is ignored, except for `/whoami`. |
| `TODO_DEFAULT_TZ` | no | `UTC` | Default time zone for new users, as an IANA name such as `Europe/Berlin` or `Asia/Tehran`. Each user can change theirs with `/tz`. |
| `TODO_DEFAULT_TIME` | no | `09:00` | Time (`HH:MM`) used for a date with no time, and for the snooze button "Tomorrow". |
| `TODO_LOG_LEVEL` | no | `info` | `debug`, `info`, `warn` or `error`. Logs are text on stderr (the systemd journal). |
| `TODO_DB_URL` | no | `TODO_DB_PATH` if set, else `sqlite://$STATE_DIRECTORY/todo.db`, else `todo.db` in the working directory | Database URL. Version 0.1 supports SQLite, for example `sqlite:///var/lib/todo-bot/todo.db`. |
| `TODO_DB_PATH` | no | | Deprecated. A plain SQLite file path, used only if `TODO_DB_URL` is unset. |
| `STATE_DIRECTORY` | no | | Set by systemd (`StateDirectory=todo-bot`). If neither `TODO_DB_URL` nor `TODO_DB_PATH` is set, the database is `$STATE_DIRECTORY/todo.db`, otherwise `todo.db` in the working directory. |
| `TODO_NODE_ID` | no | the hostname | Unique name of this process in leases and claims. Keep one stable name per host. |
| `TODO_MODE` | no | `polling` | `polling`. The value `webhook` is reserved for v0.2 and the bot refuses to start with it today. |
| `TODO_HTTP_ADDR` | webhook only | | Listen address for the webhook and the health check, for example `127.0.0.1:8081`. Reserved for v0.2. |
| `TODO_WEBHOOK_URL` | webhook only | | Public `https` URL that Telegram posts to. Reserved for v0.2. |
| `TODO_WEBHOOK_SECRET` | webhook only | | 16 to 256 characters from `A-Za-z0-9_-`. Telegram sends it back in a header so the bot can check the sender. Reserved for v0.2. |
| `TODO_LEASE_TTL` | no | `30s` | How long the node that polls Telegram holds its lease. Only matters with several nodes. |
| `TODO_CLAIM_TTL` | no | `2m` | How long a node reserves a due reminder or digest while it sends it. The minimum is `20s`. |
| `TODO_SCHED_MAX_SLEEP` | no | `5m` | Longest time the scheduler sleeps between checks. About `30s` is recommended with several nodes. |
| `GOMEMLIMIT` | no | | Go memory limit. The systemd unit in `deploy/` sets `40MiB`. |

Durations use Go syntax (`30s`, `2m`). Digits in `TODO_ALLOWED_USERS` and `TODO_DEFAULT_TIME` may be Persian.

## Documentation

- [Usage](docs/USAGE.md): every command, with English and Farsi examples.
- [راهنمای فارسی](README.fa.md)
- [Deployment](docs/DEPLOYMENT.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Changelog](CHANGELOG.md)

## Development

The code is Go. The design is in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), and the rules for contributors are in [CLAUDE.md](CLAUDE.md).

```
make ci            # vet, golangci-lint, go test -race, static linux build
make test          # go test -race ./...
make lint          # golangci-lint (built into ./bin on first use)
make build         # dist/todo-bot for this machine
make build-linux   # dist/todo-bot-linux-amd64 (ARCH=arm64 for ARM)
```

The binary also has a few subcommands: `todo-bot run` (the default), `version`, `migrate` and `backup PATH`.

Rules to remember:

- Use a separate bot token for development (see the warning in the quick start).
- Every user-facing string is registered in English and in Farsi.
- Tests use a fake clock, a fake Telegram sender and a real SQLite database in a temporary directory.

## License

[MIT](LICENSE)

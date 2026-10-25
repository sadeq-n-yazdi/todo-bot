# Deployment

Design: [ARCHITECTURE.md section 8](ARCHITECTURE.md#8-deployment). The target is a small Debian 13 VPS (systemd 257, about 860MB of RAM). Everything is driven from your workstation by `scripts/deploy.sh` over ssh/scp.

## Files

| File | Installed as |
| --- | --- |
| `deploy/todo-bot.service` | `/etc/systemd/system/todo-bot.service` |
| `deploy/todo-bot.sysusers` | `/etc/sysusers.d/todo-bot.conf` (creates the `todo-bot` user) |
| `deploy/env.example` | `/etc/todo-bot/env` (root:root, mode 0600, only if missing) |
| binary | `/opt/todo-bot/todo-bot` (previous one kept as `todo-bot.prev`) |
| data | `/var/lib/todo-bot/` (`StateDirectory=todo-bot`), backups in `/var/lib/todo-bot/backups/` |

## Script settings

`scripts/deploy.sh` reads these variables:

- `HOST` (default `root@87.106.65.34`)
- `PORT` (default `2524`)
- `ARCH` (default: detected with `uname -m` on the host)
- `LOG_LINES` and `FOLLOW=1` (for `logs`)
- `HEALTH_WAIT` (seconds to watch the service after a restart, default 12)

Example: `HOST=root@other PORT=22 scripts/deploy.sh status`. `make deploy CMD=update` is a shortcut.

## Bootstrap (first install)

1. `scripts/deploy.sh bootstrap`. It builds the binary (`make build-linux`), copies it and the unit files, creates the user, and installs the env template if `/etc/todo-bot/env` does not exist.
2. On the host, edit `/etc/todo-bot/env`: set `TELEGRAM_TOKEN` and `TODO_ALLOWED_USERS`. While the placeholder token is still there, `bootstrap` installs everything but refuses to start the service.
3. Run `scripts/deploy.sh bootstrap` again (or `systemctl enable --now todo-bot` on the host), then `scripts/deploy.sh status` and `logs`.

Re-running `bootstrap` on an installed host keeps the old binary as `todo-bot.prev` and restarts the service only if it was already running. Use `update` for normal upgrades.

## Update

`scripts/deploy.sh update`:

1. builds and `scp`s the new binary to `/opt/todo-bot/todo-bot.new`;
2. backs up the database with `todo-bot backup` into `/var/lib/todo-bot/backups/` (run as the `todo-bot` user; the newest 10 backups are kept). A symlinked or non-directory state dir aborts the update; a state dir that does not exist yet is created and has nothing to back up;
3. copies the current binary to `todo-bot.prev`, but only if the running service is healthy (active, `NRestarts` unchanged). After a failed deploy, `.prev` therefore still holds the last good binary;
4. moves the new binary into place with an atomic `mv`;
5. restarts the service and watches it for about 12s (`HEALTH_WAIT`). It must be `active/running` and `NRestarts` must not increase. Otherwise the script prints `deploy: FAILED ...` plus the journal tail and exits non-zero.

The script only reads `TODO_DB_PATH` and `TODO_DB_URL` from the env file; it never sources it.

## Rollback

`scripts/deploy.sh rollback` puts `todo-bot.prev` back and restarts the service, with the same health check and the same `FAILED` output. Only one previous version is kept. Database backups are not restored automatically: to restore one, stop the service and copy the file from `/var/lib/todo-bot/backups/` over `todo.db` (as the `todo-bot` user).

## First-start smoke test

The syscall filter cannot be proven statically, so after the first start on a host check:

- `journalctl -u todo-bot | grep -i "operation not permitted"` prints nothing unexpected.
- `ls -l /var/lib/todo-bot` shows `todo.db` and, while the bot is running, the `todo.db-wal` and `todo.db-shm` files (SQLite WAL works).
- RSS stays under 40MB: `ps -o rss= -p "$(systemctl show -p MainPID --value todo-bot)"` is below 40960 (KB).

## Status and logs

- `scripts/deploy.sh status` shows `systemctl status`.
- `scripts/deploy.sh logs` shows the last `LOG_LINES` journal lines; `FOLLOW=1` follows.

## Configuration

`/etc/todo-bot/env` is documented in `deploy/env.example`. Besides the settings in ARCHITECTURE section 7 it has `TODO_DB_URL` (for example `sqlite:///var/lib/todo-bot/todo.db`) and `TODO_NODE_ID`. v0.1 runs on a single host; the two-node setup is tracked in #34.

## Web UI

The bot can serve a web app: a Telegram Mini App at `/tg/` and a browser app (PWA) at `/`. The design is in [WEB-DESIGN.md](WEB-DESIGN.md). The web app is off by default. It starts only when `TODO_WEB_ADDR` has a value.

The bot listens on a loopback address with plain HTTP. Caddy and Cloudflare are in front of it, and they end TLS. The public origin of the maintainer's server is `https://todo.sadeq.uk`. Use your own host name in the steps below.

### Web settings

Put these variables in `/etc/todo-bot/env`. If a value is not correct, the bot does not start.

| Variable | Default | Rule |
| --- | --- | --- |
| `TODO_WEB_ADDR` | empty (web off) | A `host:port` with a loopback IP address, for example `127.0.0.1:8088`, and not equal to `TODO_HTTP_ADDR`. |
| `TODO_WEB_ORIGIN` | none | The public `https://` URL with no path, for example `https://todo.sadeq.uk`, necessary when `TODO_WEB_ADDR` has a value. |
| `TODO_WEB_SESSION_TTL` | `720h` | The idle limit of a browser session, from `1h` to `2160h`. |
| `TODO_WEB_SESSION_MAX` | `2160h` | The absolute limit of a browser session, equal to or more than `TODO_WEB_SESSION_TTL`. |
| `TODO_WEB_MEMBER_TTL` | `10m` | The time that an answer of `getChatMember` stays valid, from `1m` to `1h`. |
| `TODO_WEB_TRUSTED_PROXIES` | `127.0.0.1/32,::1/128` | The CIDR list of the proxies whose forwarded headers the bot reads. |

The code sets more limits. A Mini App session stays valid for 12 hours. A login code stays valid for 5 minutes, and it works one time. A user can have 5 Mini App sessions and 20 browser sessions. A new session removes the oldest session of the same type.

At shutdown, the web server stops in the same 10-second budget as the poller and the scheduler.

### The contract with Caddy

Requests:

| Item | Contract |
| --- | --- |
| Upstream | `http://127.0.0.1:8088`, plain HTTP on loopback. |
| Host | Caddy sends `Host: todo.sadeq.uk` with no change, and the bot refuses other hosts with 421 (but not on `/healthz` and `/readyz`). |
| Forwarded headers | Caddy sets `X-Forwarded-For` and `X-Forwarded-Proto: https`, and Cloudflare sets `CF-Connecting-IP` and `CF-IPCountry`. |
| Client IP | The bot uses `CF-Connecting-IP` from a trusted proxy and the TCP peer in all other cases, and it never reads `X-Forwarded-For`. |
| Country | The bot reads `CF-IPCountry` only from a trusted proxy, and without it the sign-in notice shows `??`. |
| Cloudflare only | Caddy accepts the site only from the IP ranges of Cloudflare or with Authenticated Origin Pulls, to stop a false `CF-Connecting-IP`. |

Responses and limits:

| Item | Contract |
| --- | --- |
| Health | `GET /healthz` gives 200 `ok` with no database read, and `GET /readyz` gives 200 after one small database read, or 503. |
| Response headers | Caddy sends `Content-Security-Policy`, `Set-Cookie`, `Cache-Control` and `ETag` with no change, and it adds no cache. |
| Compression | Caddy can use `encode zstd gzip`. |
| Body and time | A request body is 16 KiB at most, a request takes 15 seconds at most, and the bot uses no WebSockets. |
| TLS | Caddy and Cloudflare end TLS, and the bot always sets `Secure` cookies and HSTS. |

### Owner steps: BotFather

Do these steps one time. Without them, the chat with the bot has no **Open** button, and users cannot open the web app or get a login code.

1. Open @BotFather and select the bot.
2. Open **Bot Settings**.
3. Open **Configure Mini App**.
4. Turn on the Main Mini App with the URL `https://todo.sadeq.uk/tg/`.
5. Go back to **Bot Settings** and open **Menu Button**.
6. Set the URL `https://todo.sadeq.uk/tg/` and the title `Open`.
7. Before you turn on the Telegram Login Widget (a later phase), send `/setdomain` and give `todo.sadeq.uk`.
8. Do steps 1 to 7 again for the development bot, with its own host name.

The `t.me/<bot>?startapp=...` links need the Main Mini App.

### Owner steps: Cloudflare

1. Add an `A` record `todo` for the IP address of the server. Set it to **Proxied**.
2. Set **SSL/TLS** to **Full (strict)**.
3. Turn off **Rocket Loader**, **Email Obfuscation** and **Auto Minify** for `todo.sadeq.uk`. These features add scripts, and the Content Security Policy blocks them.
4. Add a cache rule that bypasses the cache for `todo.sadeq.uk/api/*`, `/auth/*` and `/sw.js`.
5. Make sure that Bot Fight Mode and challenges do not apply to `todo.sadeq.uk/api/*`. A challenge stops the requests of the app.

### Owner steps: Caddy

Do these steps in the repository of the proxy.

1. Add a site block for `todo.sadeq.uk` that sends the requests to `127.0.0.1:8088`.
2. Accept the site only from the IP ranges of Cloudflare, or turn on Authenticated Origin Pulls.
3. Keep the other rules of [the contract with Caddy](#the-contract-with-caddy).

### Owner steps: the server and the bot

1. Run the bot on one host only. Stop the bot on the old host before you start it on the new host. Two pollers with one token get 409, and two hosts cannot share one SQLite file.
2. Run `scripts/deploy.sh` with `HOST` set to the root SSH address of the new host.
3. Add `TODO_WEB_ADDR=127.0.0.1:8088` and `TODO_WEB_ORIGIN=https://todo.sadeq.uk` to `/etc/todo-bot/env`.
4. Restart the service.
5. Make sure that `curl -s http://127.0.0.1:8088/healthz` gives `ok`.

### If the bot token leaks

The bot token also signs the Mini App data. Thus a leaked token lets an attacker sign in to the web app.

1. Get a new token with the BotFather command `/revoke`.
2. Put the new token in `/etc/todo-bot/env`.
3. Stop the service.
4. Delete all web sessions: `sqlite3 /var/lib/todo-bot/todo.db "DELETE FROM web_sessions;"`.
5. Start the service.

## Unit hardening

The unit sets `ProtectSystem=strict`, `SystemCallFilter=@system-service`, `MemoryMax=96M` and `GOMEMLIMIT=40MiB`, plus the usual `Protect*`/`Restrict*` options. `SystemCallErrorNumber=EPERM` is mandatory: without it a blocked syscall kills the process with SIGSYS instead of returning an error.

Check it locally (no server needed) with `make check-deploy`: it runs `bash -n`, `shellcheck` and `systemd-analyze verify`/`security` when installed. The expected exposure is 2.0 or lower (currently 1.1).

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

## Unit hardening

The unit sets `ProtectSystem=strict`, `SystemCallFilter=@system-service`, `MemoryMax=96M` and `GOMEMLIMIT=40MiB`, plus the usual `Protect*`/`Restrict*` options. `SystemCallErrorNumber=EPERM` is mandatory: without it a blocked syscall kills the process with SIGSYS instead of returning an error.

Check it locally (no server needed) with `make check-deploy`: it runs `bash -n`, `shellcheck` and `systemd-analyze verify`/`security` when installed. The expected exposure is 2.0 or lower (currently 1.1).

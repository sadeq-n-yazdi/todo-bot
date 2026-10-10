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
- `LINES` and `FOLLOW=1` (for `logs`)

Example: `HOST=root@other PORT=22 scripts/deploy.sh status`. `make deploy CMD=update` is a shortcut.

## Bootstrap (first install)

1. `scripts/deploy.sh bootstrap`. It builds the binary (`make build-linux`), copies it and the unit files, creates the user, and installs the env template if `/etc/todo-bot/env` does not exist.
2. On the host, edit `/etc/todo-bot/env`: set `TELEGRAM_TOKEN` and `TODO_ALLOWED_USERS`.
3. `systemctl enable --now todo-bot` on the host. Re-running `bootstrap` once the env file exists enables and starts the service.
4. `scripts/deploy.sh status` and `scripts/deploy.sh logs` to confirm.

## Update

`scripts/deploy.sh update`:

1. builds and `scp`s the new binary to `/opt/todo-bot/todo-bot.new`;
2. backs up the database with `todo-bot backup` into `/var/lib/todo-bot/backups/`;
3. copies the current binary to `todo-bot.prev`;
4. moves the new binary into place with an atomic `mv`;
5. restarts the service, then checks `systemctl is-active` and prints the journal.

If the health check fails the script exits non-zero and tells you to roll back.

## Rollback

`scripts/deploy.sh rollback` puts `todo-bot.prev` back and restarts the service, with the same health check. Only one previous version is kept. Database backups are not restored automatically: to restore one, stop the service and copy the file from `/var/lib/todo-bot/backups/` over `todo.db` (as the `todo-bot` user).

## Status and logs

- `scripts/deploy.sh status` shows `systemctl status`.
- `scripts/deploy.sh logs` shows the last `LINES` journal lines; `FOLLOW=1` follows.

## Configuration

`/etc/todo-bot/env` is documented in `deploy/env.example`. Besides the settings in ARCHITECTURE section 7 it has `TODO_DB_URL` (for example `sqlite:///var/lib/todo-bot/todo.db`) and `TODO_NODE_ID`. v0.1 runs on a single host; the two-node setup is tracked in #34.

## Unit hardening

The unit sets `ProtectSystem=strict`, `SystemCallFilter=@system-service`, `MemoryMax=96M` and `GOMEMLIMIT=40MiB`, plus the usual `Protect*`/`Restrict*` options. `SystemCallErrorNumber=EPERM` is mandatory: without it a blocked syscall kills the process with SIGSYS instead of returning an error.

Check it locally (no server needed) with `make check-deploy`: it runs `bash -n`, `shellcheck` and `systemd-analyze verify`/`security` when installed. The expected exposure is 2.0 or lower (currently 1.1).

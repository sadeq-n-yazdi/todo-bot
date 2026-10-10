#!/usr/bin/env bash
# Deploy the todo bot to a single host over ssh/scp.
#   scripts/deploy.sh {bootstrap|update|rollback|status|logs}
# Environment: HOST (default root@87.106.65.34), PORT (default 2524),
# ARCH (default: detected with uname -m on the host),
# LINES (logs, default 100), FOLLOW=1 (logs, follow).
set -euo pipefail

HOST="${HOST:-root@87.106.65.34}"
PORT="${PORT:-2524}"
APP=todo-bot
BIN_DIR=/opt/todo-bot
BIN="$BIN_DIR/todo-bot"
STATE_DIR=/var/lib/todo-bot
BACKUP_DIR="$STATE_DIR/backups"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ssh_opts=(-p "$PORT" -o BatchMode=yes)
scp_opts=(-P "$PORT" -o BatchMode=yes)

die() {
	echo "deploy: $*" >&2
	exit 1
}

remote() { ssh "${ssh_opts[@]}" "$HOST" "$@"; }

usage() {
	cat <<USAGE
usage: $0 {bootstrap|update|rollback|status|logs}
  bootstrap  install user, unit, env template and first binary
  update     back up the DB, install a new binary, restart, health-check
  rollback   restore the previous binary and restart
  status     show service status
  logs       show recent journal (LINES=100, FOLLOW=1 to follow)
env: HOST=$HOST PORT=$PORT
USAGE
}

detect_arch() {
	if [ -n "${ARCH:-}" ]; then
		echo "$ARCH"
		return
	fi
	case "$(remote uname -m)" in
	x86_64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported architecture; set ARCH=amd64|arm64" ;;
	esac
}

build() {
	local arch="$1"
	make -C "$ROOT" build-linux ARCH="$arch" >&2
	echo "$ROOT/dist/todo-bot-linux-$arch"
}

# Give the unit a moment, require it to be active, then show the journal.
health_check() {
	sleep 3
	if ! remote "systemctl is-active --quiet $APP"; then
		remote "journalctl -u $APP -n 50 --no-pager" || true
		return 1
	fi
	remote "journalctl -u $APP -n 20 --no-pager"
}

cmd_bootstrap() {
	local arch bin
	arch="$(detect_arch)"
	bin="$(build "$arch")"
	scp "${scp_opts[@]}" \
		"$ROOT/deploy/todo-bot.service" "$ROOT/deploy/todo-bot.sysusers" \
		"$ROOT/deploy/env.example" "$bin" "$HOST:/tmp/"
	remote "bash -s" "todo-bot-linux-$arch" <<'REMOTE'
set -euo pipefail
src=$1
install -D -m 0644 /tmp/todo-bot.sysusers /etc/sysusers.d/todo-bot.conf
systemd-sysusers /etc/sysusers.d/todo-bot.conf
install -D -m 0644 /tmp/todo-bot.service /etc/systemd/system/todo-bot.service
install -d -m 0755 /opt/todo-bot /etc/todo-bot
install -m 0755 "/tmp/$src" /opt/todo-bot/todo-bot
created=
if [ ! -e /etc/todo-bot/env ]; then
	install -m 0600 -o root -g root /tmp/env.example /etc/todo-bot/env
	created=1
fi
chmod 0600 /etc/todo-bot/env
rm -f /tmp/todo-bot.service /tmp/todo-bot.sysusers /tmp/env.example "/tmp/$src"
systemctl daemon-reload
if [ -n "$created" ]; then
	echo "created /etc/todo-bot/env from the template: edit it, then run: systemctl enable --now todo-bot"
else
	systemctl enable --now todo-bot
fi
REMOTE
	echo "bootstrap done."
}

cmd_update() {
	local arch bin
	arch="$(detect_arch)"
	bin="$(build "$arch")"
	scp "${scp_opts[@]}" "$bin" "$HOST:$BIN.new"
	# Unquoted heredoc: $VARS expand locally, \$VARS on the host.
	remote "bash -s" <<REMOTE
set -euo pipefail
chmod 0755 $BIN.new
if [ -x $BIN ]; then
	install -d -m 0750 -o $APP -g $APP $BACKUP_DIR
	stamp=\$(date -u +%Y%m%dT%H%M%SZ)
	set -a
	. /etc/todo-bot/env
	set +a
	runuser -u $APP -- env STATE_DIRECTORY=$STATE_DIR TODO_DB_PATH="\${TODO_DB_PATH:-$STATE_DIR/todo.db}" \\
		$BIN backup "$BACKUP_DIR/todo-\$stamp.db"
	cp -p $BIN $BIN.prev
fi
mv -f $BIN.new $BIN
systemctl restart $APP
REMOTE
	if ! health_check; then
		echo "deploy: health check failed; run '$0 rollback' to restore the previous binary" >&2
		exit 1
	fi
}

cmd_rollback() {
	remote "bash -s" <<REMOTE
set -euo pipefail
[ -x $BIN.prev ] || { echo "no previous binary at $BIN.prev" >&2; exit 1; }
cp -p $BIN.prev $BIN.new
mv -f $BIN.new $BIN
systemctl restart $APP
REMOTE
	health_check
}

cmd_status() { remote "systemctl status $APP --no-pager" || true; }

cmd_logs() {
	local follow=""
	if [ "${FOLLOW:-}" = 1 ]; then follow="-f"; fi
	remote "journalctl -u $APP -n ${LINES:-100} --no-pager $follow"
}

case "${1:-}" in
bootstrap) cmd_bootstrap ;;
update) cmd_update ;;
rollback) cmd_rollback ;;
status) cmd_status ;;
logs) cmd_logs ;;
-h | --help | help) usage ;;
*)
	usage >&2
	exit 2
	;;
esac

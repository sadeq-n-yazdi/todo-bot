#!/usr/bin/env bash
# Deploy the todo bot to a single host over ssh/scp.
#   scripts/deploy.sh {bootstrap|update|rollback|status|logs}
# Environment: HOST (default root@87.106.65.34), PORT (default 2524),
# ARCH (default: detected with uname -m on the host),
# LOG_LINES (logs, default 100), FOLLOW=1 (logs, follow),
# HEALTH_WAIT (seconds to watch the service after a restart, default 12).
set -euo pipefail

HOST="${HOST:-root@87.106.65.34}"
PORT="${PORT:-2524}"
APP=todo-bot
BIN_DIR=/opt/todo-bot
BIN="$BIN_DIR/todo-bot"
STATE_DIR=/var/lib/todo-bot
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ssh_opts=(-p "$PORT" -o BatchMode=yes)
scp_opts=(-P "$PORT" -o BatchMode=yes)

die() {
	echo "deploy: $*" >&2
	exit 1
}

# Arguments are expanded on the client on purpose.
# shellcheck disable=SC2029
remote() { ssh "${ssh_opts[@]}" "$HOST" "$@"; }

usage() {
	cat <<USAGE
usage: $0 {bootstrap|update|rollback|status|logs}
  bootstrap  install user, unit, env template and binary
  update     back up the DB, install a new binary, restart, health-check
  rollback   restore the previous binary and restart
  status     show service status
  logs       show recent journal (LOG_LINES=100, FOLLOW=1 to follow)
env: HOST=$HOST PORT=$PORT
USAGE
}

detect_arch() {
	local machine
	if [ -n "${ARCH:-}" ]; then
		echo "$ARCH"
		return
	fi
	machine="$(remote uname -m)" || die "cannot reach $HOST over ssh (port $PORT)"
	case "$machine" in
	x86_64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported architecture '$machine'; set ARCH=amd64|arm64" ;;
	esac
}

build() {
	local arch="$1"
	make -C "$ROOT" build-linux ARCH="$arch" >&2
	echo "$ROOT/dist/todo-bot-linux-$arch"
}

# Watch the unit for HEALTH_WAIT seconds (longer than one RestartSec=5s cycle,
# so a crash loop is caught). Fail if it is not active/running or restarted.
health_check() {
	local label="$1" base out active sub now
	base="$(remote "systemctl show -p NRestarts --value $APP")" || die "cannot reach $HOST"
	sleep "${HEALTH_WAIT:-12}"
	out="$(remote "systemctl show -p ActiveState -p SubState -p NRestarts $APP")" || die "cannot reach $HOST"
	active="$(sed -n 's/^ActiveState=//p' <<<"$out")"
	sub="$(sed -n 's/^SubState=//p' <<<"$out")"
	now="$(sed -n 's/^NRestarts=//p' <<<"$out")"
	if [ "$active" != active ] || [ "$sub" != running ] || [ "$now" != "$base" ]; then
		echo "deploy: FAILED $label: state=$active/$sub restarts=$base->$now" >&2
		remote "journalctl -u $APP -n 50 --no-pager" || true
		return 1
	fi
	remote "journalctl -u $APP -n 20 --no-pager"
	echo "deploy: $label OK"
}

cmd_bootstrap() {
	local arch bin stage rc=0
	arch="$(detect_arch)"
	bin="$(build "$arch")"
	# Private root-owned (0700) staging dir: never install from fixed /tmp names.
	stage="$(remote "mktemp -d")"
	case "$stage" in /tmp/tmp.*) ;; *) die "unexpected staging dir: $stage" ;; esac
	scp "${scp_opts[@]}" \
		"$ROOT/deploy/todo-bot.service" "$ROOT/deploy/todo-bot.sysusers" \
		"$ROOT/deploy/env.example" "$bin" "$HOST:$stage/"
	# Exit code 3 from the host means "installed, but not started".
	remote "bash -s" "todo-bot-linux-$arch" "$stage" <<'REMOTE' || rc=$?
set -euo pipefail
src=$1
d=$2
trap 'rm -rf "$d"' EXIT
bin=/opt/todo-bot/todo-bot
env=/etc/todo-bot/env
was_active=
if systemctl is-active --quiet todo-bot; then was_active=1; fi
install -D -m 0644 "$d/todo-bot.sysusers" /etc/sysusers.d/todo-bot.conf
systemd-sysusers /etc/sysusers.d/todo-bot.conf
install -D -m 0644 "$d/todo-bot.service" /etc/systemd/system/todo-bot.service
install -d -m 0755 /opt/todo-bot /etc/todo-bot
if [ -x "$bin" ]; then
	cp -p "$bin" "$bin.prev"
fi
install -m 0755 "$d/$src" "$bin.new"
mv -f "$bin.new" "$bin"
if [ ! -e "$env" ]; then
	install -m 0600 -o root -g root "$d/env.example" "$env"
fi
chmod 0600 "$env"
systemctl daemon-reload
if grep -q '^TELEGRAM_TOKEN=123456:replace-me' "$env"; then
	echo "$env still has the placeholder token: edit it, then run: systemctl enable --now todo-bot"
	exit 3
fi
if [ -n "$was_active" ]; then
	systemctl restart todo-bot
else
	systemctl enable --now todo-bot
fi
REMOTE
	case "$rc" in
	0) health_check bootstrap ;;
	3) echo "deploy: bootstrap installed files; service not started" ;;
	*) die "bootstrap failed on the host (exit $rc)" ;;
	esac
}

cmd_update() {
	local arch bin
	arch="$(detect_arch)"
	bin="$(build "$arch")"
	scp "${scp_opts[@]}" "$bin" "$HOST:$BIN.new"
	# Quoted heredoc; arguments are binary, state dir, service name.
	remote "bash -s" "$BIN" "$STATE_DIR" "$APP" /etc/todo-bot/env <<'REMOTE'
set -euo pipefail
bin=$1
state=$2
app=$3
envfile=$4
backups=$state/backups
chmod 0755 "$bin.new"
if [ -x "$bin" ]; then
	# Is the running service healthy? Only then is its binary worth keeping.
	healthy=
	if systemctl is-active --quiet "$app"; then
		r1=$(systemctl show -p NRestarts --value "$app")
		sleep 3
		r2=$(systemctl show -p NRestarts --value "$app")
		if systemctl is-active --quiet "$app" && [ "$r1" = "$r2" ]; then healthy=1; fi
	fi

	# Root never chown/chmods anything under $state (the service user owns it
	# and could plant symlinks): state operations run as the service user.
	if [ -L "$state" ] || { [ -e "$state" ] && [ ! -d "$state" ]; }; then
		echo "deploy: $state is a symlink or not a directory; refusing to continue" >&2
		exit 1
	fi
	if [ -d "$state" ]; then
		[ "$(stat -c %U:%a "$envfile")" = root:600 ] || { echo "deploy: $envfile must be root:root 0600" >&2; exit 1; }
		# Read only the DB settings; never source the file.
		db_path=$(sed -n 's/^TODO_DB_PATH=//p' "$envfile" | tail -n 1 | tr -d '\r')
		db_url=$(sed -n 's/^TODO_DB_URL=//p' "$envfile" | tail -n 1 | tr -d '\r')
		envargs=(STATE_DIRECTORY="$state")
		if [ -n "$db_path" ]; then envargs+=(TODO_DB_PATH="$db_path"); fi
		if [ -n "$db_url" ]; then envargs+=(TODO_DB_URL="$db_url"); fi
		# The backup command fails if the database file is missing (the state
		# directory can exist before the bot ever created the database). Find
		# the sqlite file the same way the bot does and test it as the service
		# user. A non-sqlite URL, or a path that is not a plain absolute path,
		# is not checked: the backup command decides.
		# The bot rejects a sqlite URL with a query, a fragment or credentials
		# (internal/store/sqlite urlPath), so such a URL has no database path.
		# Quotes are refused too: the bot would read them as part of the path.
		dbfile=
		if [ -n "$db_url" ]; then
			case $db_url in
			sqlite:*[?#\"\']* | [\"\']*sqlite:*)
				echo "deploy: TODO_DB_URL in $envfile has a query, a fragment or quotes; the bot rejects it and deploy.sh does not support it" >&2
				exit 1
				;;
			sqlite:///*) dbfile=${db_url#sqlite://} ;;
			esac
		elif [ -n "$db_path" ]; then
			dbfile=$db_path
		else
			dbfile=$state/todo.db
		fi
		case $dbfile in
		/* | "") ;;
		*) dbfile= ;;
		esac
		case $dbfile in
		*%*) dbfile= ;;
		esac
		# Exit 10 means "the file is missing". Any other failure, including
		# runuser itself failing (it exits 1 for a missing user, as test does
		# for a missing file), must stop the deploy: do not skip the backup.
		exists=0
		if [ -n "$dbfile" ]; then
			runuser -u "$app" -- sh -c 'if [ -e "$1" ]; then exit 0; fi; exit 10' sh "$dbfile" || exists=$?
		fi
		case $exists in
		0)
			stamp=$(date -u +%Y%m%dT%H%M%SZ)
			runuser -u "$app" -- mkdir -p -m 0750 "$backups"
			runuser -u "$app" -- env "${envargs[@]}" "$bin" backup "$backups/todo-$stamp.db"
			# Keep the newest 10 backups.
			runuser -u "$app" -- sh -c 'ls -1t "$1"/todo-*.db | tail -n +11 | xargs -r rm -f --' sh "$backups"
			;;
		10) echo "deploy: no database yet ($dbfile); nothing to back up" ;;
		*)
			echo "deploy: cannot check for the database $dbfile as user $app (status $exists); refusing to deploy without a backup" >&2
			exit 1
			;;
		esac
	else
		install -d -o "$app" -g "$app" -m 0750 "$state"
		echo "deploy: $state did not exist; nothing to back up"
	fi

	if [ -n "$healthy" ]; then
		cp -p "$bin" "$bin.prev"
	else
		echo "deploy: running service not healthy; keeping existing $bin.prev" >&2
	fi
fi
mv -f "$bin.new" "$bin"
systemctl restart "$app"
REMOTE
	if ! health_check update; then
		echo "deploy: run '$0 rollback' to restore the previous binary" >&2
		exit 1
	fi
}

cmd_rollback() {
	remote "bash -s" "$BIN" "$APP" <<'REMOTE'
set -euo pipefail
bin=$1
app=$2
[ -x "$bin.prev" ] || { echo "deploy: no previous binary at $bin.prev" >&2; exit 1; }
cp -p "$bin.prev" "$bin.new"
mv -f "$bin.new" "$bin"
systemctl restart "$app"
REMOTE
	health_check rollback || exit 1
}

cmd_status() { remote "systemctl status $APP --no-pager" || true; }

cmd_logs() {
	local follow=""
	if [ "${FOLLOW:-}" = 1 ]; then follow="-f"; fi
	remote "journalctl -u $APP -n ${LOG_LINES:-100} --no-pager $follow"
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

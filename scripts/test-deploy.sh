#!/usr/bin/env bash
# Tests for the remote part of "deploy.sh update": the database-existence
# check and the backup decision. It extracts the remote script from deploy.sh
# and runs it locally in a temp dir with shims for runuser, systemctl, stat
# and sleep. It never contacts a server.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/shims"

# The remote script of cmd_update: the heredoc after "cmd_update()".
awk '
	/^cmd_update\(\)/ { in_fn = 1 }
	in_fn && /<<.REMOTE.$/ { grab = 1; next }
	grab && /^REMOTE$/ { exit }
	grab { print }
' scripts/deploy.sh >"$work/remote.sh"
[ -s "$work/remote.sh" ] || { echo "FAIL: cannot extract the remote script" >&2; exit 1; }

# runuser -u USER -- CMD...: RUNUSER_STATUS makes the shim fail like runuser.
cat >"$work/shims/runuser" <<'SHIM'
#!/usr/bin/env bash
if [ -n "${RUNUSER_STATUS:-}" ]; then
	echo "runuser: failed to execute (shim)" >&2
	exit "$RUNUSER_STATUS"
fi
shift 3
exec "$@"
SHIM
cat >"$work/shims/systemctl" <<'SHIM'
#!/usr/bin/env bash
case $1 in
is-active) exit 0 ;;
show) echo 0 ;;
*) echo "systemctl $*" >>"$SHIM_LOG" ;;
esac
SHIM
cat >"$work/shims/stat" <<'SHIM'
#!/usr/bin/env bash
echo root:600
SHIM
printf '#!/usr/bin/env bash\nexit 0\n' >"$work/shims/sleep"
chmod +x "$work/shims/"*

fail=0
# run NAME EXPECT_STATUS EXPECT_BACKUP(yes|no) EXPECT_TEXT ENV_LINES [DBFILE_PRESENT]
run() {
	local name=$1 want_rc=$2 want_backup=$3 want_text=$4 envlines=$5 present=${6:-} lock=${7:-}
	local d="$work/case" rc=0 out
	rm -rf "$d"
	mkdir -p "$d/state" "$d/opt"
	printf '%b' "$envlines" >"$d/env"
	printf '#!/usr/bin/env bash\necho "$@" >>"%s"\n' "$d/backup.log" >"$d/opt/todo-bot"
	chmod +x "$d/opt/todo-bot"
	cp "$d/opt/todo-bot" "$d/opt/todo-bot.new"
	if [ -n "$present" ]; then mkdir -p "$(dirname "$present")"; : >"$present"; fi
	if [ -n "$lock" ]; then chmod 000 "$lock"; fi
	out="$(SHIM_LOG="$d/shim.log" PATH="$work/shims:$PATH" \
		bash "$work/remote.sh" "$d/opt/todo-bot" "$d/state" todo-bot "$d/env" 2>&1)" || rc=$?
	if [ -n "$lock" ]; then chmod 755 "$lock"; fi
	local got_backup=no
	if [ -s "$d/backup.log" ]; then got_backup=yes; fi
	if [ "$rc" != "$want_rc" ] || [ "$got_backup" != "$want_backup" ] || ! grep -Fq -- "$want_text" <<<"$out"; then
		echo "FAIL: $name: rc=$rc (want $want_rc) backup=$got_backup (want $want_backup) text '$want_text'" >&2
		printf "    %s\n" "${out//$'\n'/$'\n    '}" >&2
		fail=1
	else
		echo "ok: $name"
	fi
}

d="$work/case"
RUNUSER_STATUS='' run "db missing: skip backup, continue" 0 no "no database yet" \
	"TODO_DB_URL=sqlite://$d/state/todo.db\n"
RUNUSER_STATUS='' run "db present: back up" 0 yes "" \
	"TODO_DB_URL=sqlite://$d/state/todo.db\n" "$d/state/todo.db"
RUNUSER_STATUS='' run "default path, db missing" 0 no "no database yet" ""
RUNUSER_STATUS='' run "TODO_DB_PATH, db present" 0 yes "" \
	"TODO_DB_PATH=$d/state/x.db\n" "$d/state/x.db"
RUNUSER_STATUS=1 run "runuser fails with 1 (missing user): stop" 1 no "cannot check for the database" \
	"TODO_DB_URL=sqlite://$d/state/todo.db\n"
RUNUSER_STATUS=127 run "runuser fails with 127: stop" 1 no "cannot check for the database" ""
RUNUSER_STATUS='' run "URL with query: stop, no 'no database yet'" 1 no "has a query or fragment" \
	"TODO_DB_URL=sqlite://$d/state/todo.db?x=1\n"
RUNUSER_STATUS='' run "URL with fragment: stop" 1 no "has a query or fragment" \
	"TODO_DB_URL=sqlite://$d/state/todo.db#f\n"
RUNUSER_STATUS='' run "URL with double quotes: stop" 1 no "does not support quotes" \
	"TODO_DB_URL=\"sqlite://$d/state/todo.db\"\n"
RUNUSER_STATUS='' run "URL with single quotes: stop" 1 no "does not support quotes" \
	"TODO_DB_URL='sqlite://$d/state/todo.db'\n"

# An existing DB under an unsearchable directory must stop the deploy, not
# read as "missing". chmod does not block root, so skip then.
if [ "$(id -u)" = 0 ]; then
	echo "skip: unsearchable parent directory (running as root)"
else
	RUNUSER_STATUS='' run "db under unsearchable dir: stop" 1 no "cannot check for the database" \
		"TODO_DB_URL=sqlite://$d/state/sub/todo.db\n" "$d/state/sub/todo.db" "$d/state/sub"
fi

# require_webui: the deploy stops when the binary has no webui build tag.
# It runs the function from deploy.sh with a shim for "go version -m".
awk '/^require_webui\(\)/ { grab = 1 } grab { print } grab && /^}/ { exit }' scripts/deploy.sh >"$work/webui.sh"
[ -s "$work/webui.sh" ] || { echo "FAIL: cannot extract require_webui" >&2; exit 1; }
mkdir -p "$work/gobin"
cat >"$work/gobin/go" <<'SHIM'
#!/usr/bin/env bash
[ "$1 $2" = "version -m" ] || exit 2
printf '%s: go1.27\n\tpath\tx\n' "$3"
printf '%b' "${GO_BUILD_LINES:-}"
SHIM
chmod +x "$work/gobin/go"
# webui_case NAME EXPECT_STATUS BUILD_LINES
webui_case() {
	local name=$1 want=$2 lines=$3 rc=0 out
	out="$(GO_BUILD_LINES="$lines" PATH="$work/gobin:$PATH" bash -c '
		die() { echo "deploy: $*" >&2; exit 1; }
		source "$1"
		require_webui /x/todo-bot' _ "$work/webui.sh" 2>&1)" || rc=$?
	if [ "$rc" != "$want" ]; then
		echo "FAIL: $name: rc=$rc (want $want): $out" >&2
		fail=1
	else
		echo "ok: $name"
	fi
}
webui_case "webui tag: continue" 0 '\tbuild\t-trimpath=true\n\tbuild\t-tags=webui\n'
webui_case "webui among other tags: continue" 0 '\tbuild\t-tags=foo,webui\n'
webui_case "no tags: stop" 1 '\tbuild\t-trimpath=true\n'
webui_case "other tag only: stop" 1 '\tbuild\t-tags=nowebui\n'

exit "$fail"

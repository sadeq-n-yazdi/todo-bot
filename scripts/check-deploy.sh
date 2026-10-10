#!/usr/bin/env bash
# Static checks for the deployment files. Never contacts a server.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

fail=0
check() {
	if ! grep -Eq "$2" "$1"; then
		echo "FAIL: $1 lacks /$2/" >&2
		fail=1
	fi
}

bash -n scripts/deploy.sh
if command -v shellcheck >/dev/null; then
	shellcheck scripts/deploy.sh scripts/check-deploy.sh
else
	echo "shellcheck not installed; skipped"
fi

u=deploy/todo-bot.service
for re in '^SystemCallErrorNumber=EPERM$' '^SystemCallFilter=@system-service$' \
	'^ProtectSystem=strict$' '^MemoryMax=96M$' '^Environment=GOMEMLIMIT=40MiB$' \
	'^Restart=on-failure$' '^StateDirectory=todo-bot$' '^User=todo-bot$'; do
	check "$u" "$re"
done
check deploy/todo-bot.sysusers '^u todo-bot '
check deploy/env.example '^TODO_DB_URL=sqlite:///'
check deploy/env.example '^TODO_NODE_ID='

if command -v systemd-analyze >/dev/null; then
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT
	cp "$u" "$tmp/"
	# The binary does not exist locally; ignore only that complaint.
	out="$(systemd-analyze verify "$tmp/todo-bot.service" 2>&1 || true)"
	if echo "$out" | grep -v 'is not executable' | grep -q .; then
		echo "$out" >&2
		fail=1
	fi
	score="$(systemd-analyze security --offline=true "$tmp/todo-bot.service" 2>&1 | sed -n 's/.*exposure level for [^:]*: \([0-9.]*\).*/\1/p' || true)"
	echo "exposure: ${score:-unknown}"
	if [ -n "$score" ] && awk -v s="$score" 'BEGIN{exit !(s > 2.0)}'; then
		echo "FAIL: exposure $score > 2.0" >&2
		fail=1
	fi
fi
exit "$fail"

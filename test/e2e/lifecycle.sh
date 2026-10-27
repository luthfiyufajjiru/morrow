#!/bin/sh
# Process lifecycle checks (no root needed): graceful stop, stop timeout,
# restart on crash with backoff, giving up, and cancelling a pending restart.
#
#   sh test/e2e/lifecycle.sh <morrow-binary>
set -u
SRC="${1:-}"
[ -f "$SRC" ] || { echo "usage: sh lifecycle.sh <morrow-binary>"; exit 2; }
H=$(mktemp -d)
export MORROW_HOME="$H"
M="$H/morrow"
cp "$SRC" "$M" && chmod +x "$M"
cleanup() {
  for a in graceful stubborn crashy loop plain clean failing; do "$M" stop --timeout 1s "$a" >/dev/null 2>&1; done
  pkill -f "morrow-e2e-lifecycle" 2>/dev/null
  rm -rf "$H"
}
trap cleanup EXIT

ms() { date +%s%3N; }
pid_of() { "$M" status "$1" | sed -n 's/^PID: //p'; }
status_of() { "$M" status "$1" | sed -n 's/^Status: //p'; }
pass=0; fail=0
ok() { if eval "$2"; then pass=$((pass+1)); echo "PASS  $1"; else fail=$((fail+1)); echo "FAIL  $1"; fi; }
# Tag every test process so cleanup can find strays.
TAG=": morrow-e2e-lifecycle;"

"$M" init >/dev/null
"$M" create graceful /bin/sh -c "$TAG trap 'echo got SIGTERM; sleep 1; echo bye; exit 0' TERM; while :; do sleep 0.2; done" >/dev/null
"$M" create stubborn /bin/sh -c "$TAG trap '' TERM; while :; do sleep 0.2; done" >/dev/null
"$M" create --restart unless-stopped crashy /bin/sh -c "$TAG echo up; while :; do sleep 0.2; done" >/dev/null
"$M" create --restart unless-stopped --max-restarts 2 loop /bin/sh -c "$TAG exit 3" >/dev/null
"$M" create plain /bin/sh -c "$TAG while :; do sleep 0.2; done" >/dev/null

echo "graceful stop:"
"$M" start graceful >/dev/null; sleep 0.5
t0=$(ms); "$M" stop graceful >/dev/null; dt=$(( $(ms) - t0 ))
ok "waits for the app to clean up (${dt}ms, app needs ~1000ms)" "[ $dt -ge 900 ] && [ $dt -lt 5000 ]"
ok "app saw SIGTERM and exited itself" "\"$M\" logs graceful | grep -q bye"
ok "recorded as stopped" "[ \"\$(status_of graceful)\" = stopped ]"

echo "stop timeout:"
"$M" start stubborn >/dev/null; sleep 0.5; P=$(pid_of stubborn)
t0=$(ms); "$M" stop --timeout 2s stubborn >/dev/null; dt=$(( $(ms) - t0 ))
ok "an app ignoring SIGTERM is killed after --timeout (${dt}ms)" "[ $dt -ge 1900 ] && [ $dt -lt 8000 ]"
ok "...and is really gone" "! kill -0 $P 2>/dev/null"

echo "restart on crash:"
"$M" start crashy >/dev/null; sleep 0.5; P1=$(pid_of crashy); kill -9 "$P1"; sleep 2.5; P2=$(pid_of crashy)
ok "killed app is restarted with a new PID ($P1 -> $P2)" "[ \"\$(status_of crashy)\" = running ] && [ -n \"$P2\" ] && [ \"$P2\" != \"$P1\" ] && kill -0 $P2"
ok "log explains the restart" "\"$M\" logs crashy | grep -q 'restarting in 1s (attempt 1/5)'"
kill -9 "$P2"; sleep 0.5
ok "status is 'restarting' during the backoff" "[ \"\$(status_of crashy)\" = restarting ]"
"$M" stop crashy >/dev/null; sleep 3
ok "stop during the backoff cancels the restart" "[ \"\$(status_of crashy)\" = stopped ] && \"$M\" logs crashy | grep -q 'restart cancelled'"

echo "crash loop:"
"$M" start loop >/dev/null; sleep 6
ok "gives up after --max-restarts" "[ \"\$(status_of loop)\" = exited ] && \"$M\" detail loop | grep -q 'restarted 2'"
ok "log says it gave up" "\"$M\" logs loop | grep -q 'gave up after 2 restarts'"

echo "policy no:"
"$M" start plain >/dev/null; sleep 0.5; kill -9 "$(pid_of plain)"; sleep 1.5
ok "unexpected exit is recorded as exited, not restarted" "[ \"\$(status_of plain)\" = exited ]"

echo "restart command:"
"$M" start graceful >/dev/null; sleep 0.5; P1=$(pid_of graceful)
"$M" restart graceful >/dev/null; sleep 0.5; P2=$(pid_of graceful)
ok "restart replaces the process ($P1 -> $P2)" "[ \"\$(status_of graceful)\" = running ] && [ \"$P2\" != \"$P1\" ] && ! kill -0 $P1 2>/dev/null"
"$M" stop graceful >/dev/null

echo "exit codes and on-failure:"
"$M" create --restart on-failure clean /bin/sh -c "$TAG echo run X=\$X; exit 0" >/dev/null
"$M" create --restart on-failure --max-restarts 2 failing /bin/sh -c "$TAG echo run X=\$X; exit 3" >/dev/null
HOOK_OUT="$H/hook.txt"
"$M" hook add failing --on crash,giveup --exec "echo \$MORROW_EVENT code=\$MORROW_EXIT_CODE restarts=\$MORROW_RESTART_COUNT >> '$HOOK_OUT'" >/dev/null
"$M" start clean >/dev/null; sleep 1.5
ok "on-failure does not restart a clean exit" "[ \"\$(status_of clean)\" = exited ] && [ \$(\"$M\" logs clean | grep -c 'run X=') -eq 1 ]"
ok "detail shows the real exit code 0" "\"$M\" detail clean | grep -q 'Last Exit .*exit code 0'"
"$M" start -e X=kept failing >/dev/null; sleep 5
ok "on-failure restarts exit code 3, then gives up" "[ \"\$(status_of failing)\" = exited ] && [ \$(\"$M\" logs failing | grep -c 'run X=') -eq 3 ]"
ok "start -e values survive automatic restarts" "[ \$(\"$M\" logs failing | grep -c 'run X=kept') -eq 3 ]"
ok "log names the exit code" "\"$M\" logs failing | grep -q '(exit code 3); restarting in 1s (attempt 1/2)'"
ok "crash hook ran for every crash, then giveup" "[ \$(grep -c '^crash code=3' '$HOOK_OUT') -eq 3 ] && grep -q '^giveup code=3 restarts=2' '$HOOK_OUT'"
"$M" start plain >/dev/null; sleep 0.5; kill -9 "$(pid_of plain)"; sleep 1.5
ok "detail shows death by signal" "\"$M\" detail plain | grep -q 'Last Exit .*killed by SIGKILL'"

echo "cleanup:"
ok "no stray app or relay processes" "! ps -eo args= | grep -v grep | grep -q -e 'morrow-e2e-lifecycle' -e '$M _relay'"

echo; echo "result: $pass passed, $fail failed"
[ $fail -eq 0 ]

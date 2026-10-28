#!/bin/sh
# Health check checks: an app that hangs without exiting is restarted after
# --health-failures failed checks, even with restart policy "no"; an app that
# never becomes healthy gives up after --max-restarts; removing the check
# stops the restarts.
#
#   sh test/e2e/health.sh <morrow-binary> <healthapp-binary> [run-as-account]
#
# No root needed. As root with an account, apps run with --user <account>.
set -u
SRC="${1:-}"
APP_SRC="${2:-}"
RUNAS="${3:-}"
[ -f "$SRC" ] && [ -f "$APP_SRC" ] || { echo "usage: sh health.sh <morrow-binary> <healthapp-binary> [run-as-account]"; exit 2; }
H=$(mktemp -d)
chmod 755 "$H"
export MORROW_HOME="$H"
M="$H/morrow"
APP="$H/morrow-e2e-healthapp"
cp "$SRC" "$M" && cp "$APP_SRC" "$APP" && chmod +x "$M" "$APP"
HANG="$H/hang"
HOOK_OUT="$H/hooks.out"
cleanup() {
  for a in web never; do "$M" stop --timeout 1s "$a" >/dev/null 2>&1; done
  pkill -f "morrow-e2e-healthapp" 2>/dev/null
  rm -rf "$H"
}
trap cleanup EXIT

pid_of() { "$M" status "$1" | sed -n 's/^PID: //p'; }
status_of() { "$M" status "$1" | sed -n 's/^Status: //p'; }
copies() { pgrep -f "$APP" | wc -l; }
wait_log() { i=0; while ! "$M" logs -n 0 "$1" | grep -q "$2"; do [ $i -ge $(($3 * 10)) ] && return 1; sleep 0.1; i=$((i+1)); done; }
pass=0; fail=0
ok() { if eval "$2"; then pass=$((pass+1)); echo "PASS  $1"; else fail=$((fail+1)); echo "FAIL  $1"; fi; }

U=""
[ -n "$RUNAS" ] && U="--user $RUNAS" && echo "(apps run as $RUNAS)"
"$M" init >/dev/null
P1=$((20000 + $$ % 20000)); P2=$((P1 + 1))

echo "validation:"
ok "create rejects a non-HTTP URL and leaves no app" "! \"$M\" create --health-url ftp://x/ bad /bin/true >/dev/null 2>&1 && ! \"$M\" status bad >/dev/null 2>&1"
ok "create rejects a sub-second interval" "! \"$M\" create --health-url http://127.0.0.1:1/ --health-interval 500ms bad /bin/true >/dev/null 2>&1"

echo "a healthy app:"
# Restart policy "no": the health check restarts the app regardless.
"$M" create $U --health-url "http://127.0.0.1:$P1/healthz" --health-interval 1s --health-failures 2 web "$APP" >/dev/null
"$M" set-env web PORT=$P1 HANG_FILE="$HANG" >/dev/null
"$M" hook add web --on unhealthy,giveup --exec "echo \$MORROW_EVENT restarts=\$MORROW_RESTART_COUNT >> '$HOOK_OUT'" >/dev/null
ok "detail shows the check" "\"$M\" detail web | grep -q 'GET http://127.0.0.1:$P1/healthz every 1s, restart after 2 failures'"
"$M" start web >/dev/null; W1=$(pid_of web); sleep 3.5
ok "passing checks change nothing" "[ \"\$(pid_of web)\" = $W1 ] && ! \"$M\" logs -n 0 web | grep -q 'health check failed'"

echo "the app hangs (stops answering, does not exit):"
touch "$HANG"
ok "failures are logged" "wait_log web 'health check failed (1/2)' 6"
ok "after 2 failures it is stopped as unhealthy" "wait_log web 'unhealthy after 2 failed health checks' 6"
ok "...and restarted although the policy is 'no'" "wait_log web 'was stopped as unhealthy after .*restarting in 1s (attempt 1/5)' 15"
rm -f "$HANG"
ok "a new instance runs, one copy" "wait_log web 'restarted with PID' 5 && sleep 0.5 && [ \"\$(status_of web)\" = running ] && [ \"\$(pid_of web)\" != $W1 ] && [ \$(copies) -eq 1 ]"
ok "the unhealthy hook ran" "sleep 0.5; grep -q '^unhealthy restarts=1' '$HOOK_OUT'"
ok "detail shows the restart" "\"$M\" detail web | grep -q 'restarted 1)'"

echo "removing the check:"
"$M" update --health-url "" web "$APP" >/dev/null
ok "detail shows no check" "\"$M\" detail web | grep -q 'Health Check *| (none)'"
"$M" restart web >/dev/null; W3=$(pid_of web)
touch "$HANG"; sleep 4
ok "a hung app is no longer restarted" "[ \"\$(pid_of web)\" = $W3 ] && [ \"\$(status_of web)\" = running ]"
rm -f "$HANG"
"$M" stop web >/dev/null

echo "an app that never becomes healthy:"
# It listens on P2, but the check looks at P1, where nothing answers.
"$M" create $U --health-url "http://127.0.0.1:$P1/healthz" --health-interval 1s --health-failures 1 --max-restarts 2 never "$APP" >/dev/null
"$M" set-env never PORT=$P2 >/dev/null
"$M" hook add never --on giveup --exec "echo \$MORROW_EVENT never restarts=\$MORROW_RESTART_COUNT >> '$HOOK_OUT'" >/dev/null
"$M" start never >/dev/null
ok "gives up after --max-restarts" "wait_log never 'gave up after 2 restarts' 20 && sleep 0.5 && [ \"\$(status_of never)\" = exited ]"
ok "the giveup hook ran" "sleep 0.5; grep -q '^giveup never restarts=2' '$HOOK_OUT'"

echo "cleanup:"
ok "no stray app or relay processes" "sleep 0.5; [ \$(copies) -eq 0 ] && ! pgrep -f '$M _relay' >/dev/null"

echo; echo "result: $pass passed, $fail failed"
[ $fail -eq 0 ]

#!/bin/sh
# Relay death checks: when an app's relay (its supervisor) is killed, by
# SIGKILL or the kernel OOM killer, the app must not keep running
# unsupervised, status must say so, and start must not launch a second copy.
#
#   sh test/e2e/relay.sh <morrow-binary> [run-as-account]
#
# No root needed. As root with an account, apps run with --user <account>.
# As root with a writable memory cgroup, the relay is also OOM-killed for real.
set -u
SRC="${1:-}"
RUNAS="${2:-}"
[ -f "$SRC" ] || { echo "usage: sh relay.sh <morrow-binary> [run-as-account]"; exit 2; }
H=$(mktemp -d)
chmod 755 "$H"
export MORROW_HOME="$H"
M="$H/morrow"
cp "$SRC" "$M" && chmod +x "$M"
APPS="quiet chatty stubborn looping racer noisy"
CG=""
cleanup() {
  for a in $APPS; do "$M" stop --timeout 1s "$a" >/dev/null 2>&1; done
  pkill -9 -f "morrow-e2e-relay" 2>/dev/null
  [ -n "$CG" ] && rmdir "$CG" 2>/dev/null
  rm -rf "$H"
}
trap cleanup EXIT

pid_of() { "$M" status "$1" | sed -n 's/^PID: //p'; }
status_of() { "$M" status "$1" | sed -n 's/^Status: //p'; }
relay_of() { cat "$H/logs/$1.relay.pid" 2>/dev/null; }
# Processes of app $1 (its tag is in the shell's command line and in its
# child's arguments), and relays for it.
copies() { pgrep -f "morrow-e2e-relay-$1" | wc -l; }
relays() { pgrep -f "$M _relay $1\$" | wc -l; }
gone_within() { i=0; while [ $i -lt $(($2 * 10)) ]; do [ "$(copies "$1")" -eq 0 ] && return 0; sleep 0.1; i=$((i + 1)); done; return 1; }
pass=0; fail=0
ok() { if eval "$2"; then pass=$((pass+1)); echo "PASS  $1"; else fail=$((fail+1)); echo "FAIL  $1"; fi; }

U=""
[ -n "$RUNAS" ] && U="--user $RUNAS"
"$M" init >/dev/null
tag() { echo ": morrow-e2e-relay-$1;"; }
# quiet writes nothing after starting; chatty writes all the time; stubborn
# ignores SIGTERM and has a child in its process group; looping crashes.
"$M" create $U --restart on-failure quiet /bin/sh -c "$(tag quiet) echo up; while :; do sleep 1; done" >/dev/null
"$M" create $U --restart on-failure chatty /bin/sh -c "$(tag chatty) while :; do echo tick; sleep 0.2; done" >/dev/null
"$M" create $U --restart on-failure stubborn /bin/sh -c "$(tag stubborn) trap '' TERM; sh -c 'while :; do sleep 1; done' morrow-e2e-relay-stubborn-child & while :; do sleep 0.2; done" >/dev/null
"$M" create $U --restart on-failure --max-restarts 10 looping /bin/sh -c "$(tag looping) exit 3" >/dev/null
"$M" create $U racer /bin/sh -c "$(tag racer) while :; do sleep 1; done" >/dev/null
"$M" create $U noisy /bin/sh -c "$(tag noisy) while :; do head -c 65536 /dev/zero | tr '\0' x; echo; sleep 0.1; done" >/dev/null
[ -n "$RUNAS" ] && echo "(apps run as $RUNAS)"

for a in quiet chatty; do
  echo "$a: kill -9 the relay:"
  "$M" start "$a" >/dev/null; sleep 0.5
  kill -9 "$(relay_of "$a")"
  ok "the app does not outlive its relay" "gone_within $a 3"
  ok "status reports exited, not running" "[ \"\$(status_of $a)\" = exited ]"
  ok "list agrees" "\"$M\" list | grep -w $a | grep -q exited"
  ok "logs say the relay died" "\"$M\" logs $a | grep -q 'the supervising relay died'"
  "$M" start "$a" >/dev/null
  ok "start runs exactly one copy under one relay" "sleep 0.5; [ \$(copies $a) -eq 1 ] && [ \$(relays $a) -eq 1 ] && [ \"\$(status_of $a)\" = running ]"
done

echo "stubborn (ignores SIGTERM, has a child): kill -9 the relay:"
"$M" start stubborn >/dev/null; sleep 0.5
kill -9 "$(relay_of stubborn)"; sleep 0.5
ok "status reports exited" "[ \"\$(status_of stubborn)\" = exited ]"
ok "...and the app and its child are stopped" "gone_within stubborn 8"
"$M" start stubborn >/dev/null
ok "start runs exactly one copy" "sleep 0.5; [ \$(copies stubborn) -eq 2 ] && [ \$(relays stubborn) -eq 1 ]"

echo "looping: kill -9 the relay while it waits to restart:"
"$M" start looping >/dev/null 2>&1
i=0; while [ "$(status_of looping)" != restarting ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i+1)); done
kill -9 "$(relay_of looping)"
ok "status leaves restarting" "[ \"\$(status_of looping)\" = exited ]"
"$M" start looping >/dev/null 2>&1
i=0; while [ "$(status_of looping)" != restarting ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i+1)); done
R1=$(relay_of looping)
"$M" start looping >/dev/null 2>&1
ok "start while it waits to restart replaces that relay" "! kill -0 $R1 2>/dev/null && [ \$(relays looping) -eq 1 ]"
"$M" stop looping >/dev/null 2>&1

echo "racer: two starts at once:"
"$M" start racer >"$H/r1" 2>&1 & "$M" start racer >"$H/r2" 2>&1 & wait
ok "one start wins, the other fails" "[ \$(cat '$H/r1' '$H/r2' | grep -c 'Successfully started') -eq 1 ]"
ok "exactly one copy runs" "sleep 0.5; [ \$(copies racer) -eq 1 ] && [ \$(relays racer) -eq 1 ]"

# A real OOM kill: move the relay into a memory cgroup with a limit below its
# usage, so the kernel's OOM killer kills it (cgroup v2, or v1 with
# move_charge so the relay's memory moves with it).
if [ "$(id -u)" = 0 ]; then
  if [ -w /sys/fs/cgroup/cgroup.procs ] && grep -qw memory /sys/fs/cgroup/cgroup.controllers 2>/dev/null; then
    # v2 does not move the relay's existing memory with it, so only a limit of
    # 0 makes its next allocation fail.
    CG=/sys/fs/cgroup/morrow-e2e-oom; mkdir -p "$CG"; LIMIT=memory.max; EVENTS=memory.events; STEPS="0"
    grep -qw memory /sys/fs/cgroup/cgroup.subtree_control || echo +memory > /sys/fs/cgroup/cgroup.subtree_control
  elif [ -d /sys/fs/cgroup/memory ] && [ -w /sys/fs/cgroup/memory/cgroup.procs ]; then
    CG=/sys/fs/cgroup/memory/morrow-e2e-oom; mkdir -p "$CG"; LIMIT=memory.limit_in_bytes; EVENTS=memory.oom_control
    # A single limit far below usage is refused (EBUSY): step down.
    STEPS="8M 4M 2M 1M 512K 256K 128K 64K 32K 16K"
    echo 1 > "$CG/memory.move_charge_at_immigrate"
  fi
fi
if [ -n "$CG" ]; then
  echo "noisy: OOM-kill the relay (it must allocate to be killed: this app writes 640 KB/s of output):"
  "$M" start noisy >"$H/noisy.start" 2>&1; sleep 0.5
  R=$(relay_of noisy)
  [ -n "$R" ] || echo "  (no relay PID: $(cat "$H/noisy.start"); status $(status_of noisy))"
  echo "$R" > "$CG/cgroup.procs"
  for l in $STEPS; do
    kill -0 "$R" 2>/dev/null || break
    echo "$l" > "$CG/$LIMIT" 2>/dev/null; sleep 0.5
  done
  i=0; while kill -0 "$R" 2>/dev/null && [ $i -lt 150 ]; do sleep 0.1; i=$((i+1)); done
  kill -0 "$R" 2>/dev/null && echo "  (relay $R survived: limit $(cat "$CG/$LIMIT"), procs $(tr '\n' ' ' < "$CG/cgroup.procs"), $(tr '\n' ' ' < "$CG/$EVENTS"))"
  ok "the kernel OOM-killed the relay" "! kill -0 $R 2>/dev/null && grep -q 'oom_kill [1-9]' '$CG/$EVENTS'"
  ok "the app does not outlive its relay" "gone_within noisy 3"
  ok "status reports exited" "[ \"\$(status_of noisy)\" = exited ]"
  "$M" start noisy >/dev/null
  ok "start runs exactly one copy" "sleep 0.5; [ \$(copies noisy) -eq 1 ] && [ \$(relays noisy) -eq 1 ]"
else
  echo "SKIP  OOM kill (needs root and a writable memory cgroup)"
fi

echo "cleanup:"
for a in $APPS; do "$M" stop --timeout 1s "$a" >/dev/null 2>&1; done
ok "no stray app or relay processes" "sleep 0.5; ! pgrep -f 'morrow-e2e-relay' >/dev/null && ! pgrep -f '$M _relay' >/dev/null"

echo; echo "result: $pass passed, $fail failed"
[ $fail -eq 0 ]

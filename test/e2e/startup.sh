#!/bin/sh
# Start-at-boot checks with a real systemd unit. Run as root on a throwaway
# machine with systemd (CI runner, VM, WSL with systemd):
#   sudo sh test/e2e/startup.sh <morrow-binary>
# It installs morrow.service and a system install, simulates a reboot by
# killing every app and relay, and removes everything at the end. It refuses
# to run if Morrow is already installed here.
set -u
SRC="${1:-}"
[ "$(id -u)" = 0 ] && [ -f "$SRC" ] || { echo "usage (as root): sh startup.sh <morrow-binary>"; exit 2; }
[ -d /run/systemd/system ] || { echo "systemd is not running"; exit 2; }
for p in /etc/morrow /var/lib/morrow /var/log/morrow /etc/systemd/system/morrow.service; do
  [ -e "$p" ] && { echo "refusing: $p exists"; exit 2; }
done

BIN=/opt/morrow-e2e-startup
M="$BIN/morrow"
TAG=": morrow-e2e-startup;"
cleanup() {
  "$M" startup --remove >/dev/null 2>&1
  rm -f /etc/systemd/system/morrow.service; systemctl daemon-reload 2>/dev/null
  for a in api web manual; do "$M" stop --timeout 1s "$a" >/dev/null 2>&1; done
  pkill -9 -f "morrow-e2e-startup" 2>/dev/null
  rm -rf /etc/morrow /var/lib/morrow /var/log/morrow "$BIN"
}
trap cleanup EXIT

mkdir -p "$BIN" && cp "$SRC" "$M" && chmod 755 "$BIN" "$M"
pid_of() { "$M" status "$1" | sed -n 's/^PID: //p'; }
status_of() { "$M" status "$1" | sed -n 's/^Status: //p'; }
pass=0; fail=0
ok() { if eval "$2"; then pass=$((pass+1)); echo "PASS  $1"; else fail=$((fail+1)); echo "FAIL  $1"; fi; }
# Kill every app and relay without telling Morrow, as a power loss would.
power_loss() {
  pkill -9 -f "$M _relay" 2>/dev/null
  pkill -9 -f "$TAG" 2>/dev/null
  sleep 1
}

"$M" init >/dev/null
for a in api web manual; do "$M" create "$a" /bin/sh -c "$TAG while :; do sleep 0.2; done" >/dev/null; done
for a in api web manual; do "$M" start "$a" >/dev/null; done
"$M" stop manual >/dev/null

echo "install:"
"$M" startup >/dev/null
ok "unit installed, enabled and active" "systemctl is-enabled morrow.service >/dev/null && systemctl is-active morrow.service >/dev/null"
ok "--status lists the wanted apps only" "\"$M\" startup --status | grep -q 'Apps started at boot: api, web'"

echo "after a simulated reboot:"
A1=$(pid_of api); power_loss
ok "apps are really gone" "! kill -0 $A1 2>/dev/null"
systemctl restart morrow.service; sleep 1
A2=$(pid_of api); W2=$(pid_of web)
ok "wanted apps are started by the unit ($A1 -> $A2)" "[ \"\$(status_of api)\" = running ] && [ \"\$(status_of web)\" = running ] && kill -0 $A2 && kill -0 $W2"
ok "an app the user stopped stays stopped" "[ \"\$(status_of manual)\" != running ]"
ok "boot is logged to the journal" "journalctl -u morrow.service --no-pager | grep -q 'boot: api: started with PID'"

echo "unit stop outside a system shutdown:"
systemctl stop morrow.service; sleep 1
ok "apps keep running" "kill -0 $A2 && [ \"\$(status_of api)\" = running ]"
systemctl start morrow.service

echo "system shutdown:"
"$M" _shutdown --always >/dev/null
ok "apps are stopped gracefully" "! kill -0 $A2 2>/dev/null && [ \"\$(status_of api)\" = stopped ] && [ \"\$(status_of web)\" = stopped ]"
ok "...and still start at the next boot" "\"$M\" startup --status | grep -q 'Apps started at boot: api, web'"
systemctl restart morrow.service; sleep 1
ok "next boot brings them back" "[ \"\$(status_of api)\" = running ] && [ \"\$(status_of web)\" = running ]"

echo "remove:"
A3=$(pid_of api)
"$M" startup --remove >/dev/null
ok "unit removed" "[ ! -e /etc/systemd/system/morrow.service ] && ! systemctl is-enabled morrow.service >/dev/null 2>&1"
ok "running apps were not stopped" "kill -0 $A3 && [ \"\$(status_of api)\" = running ]"

echo "uninstall removes the unit:"
"$M" startup >/dev/null
"$M" uninstall --yes >/dev/null
ok "unit gone after uninstall" "[ ! -e /etc/systemd/system/morrow.service ]"
"$M" init >/dev/null
ok "a reinstall still knows which apps to start" "\"$M\" startup --status | grep -q 'Apps started at boot: api, web'"

echo; echo "result: $pass passed, $fail failed"
[ $fail -eq 0 ]

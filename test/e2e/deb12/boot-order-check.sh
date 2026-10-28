#!/bin/sh
# Run as root right after rebooting a VM prepared by boot-order-setup.sh.
# Passes when the app came up on its first attempt, after PostgreSQL.
set -u
M=${MORROW:-/usr/local/bin/morrow}
for i in $(seq 1 60); do systemctl is-active -q morrow.service && break; sleep 1; done
sleep 3
echo "--- boot $(cat /proc/sys/kernel/random/boot_id), $(systemctl is-system-running)"
# Since PID 1 started: on WSL every distro boot shares the VM's kernel and boot id.
journalctl --since "$(date -d "$(ps -o lstart= -p 1)" '+%F %T')" -o short-precise --no-pager -u morrow.service -u postgresql@15-main.service | grep -Ev '^-- ' | sed 's/^[A-Za-z]* [0-9]* //'
echo "--- morrow"
"$M" status dbapp
"$M" detail --json dbapp | grep -E '"application_(status|restart_count|run_as|last_exit_code)"'
echo "--- app log"
"$M" logs dbapp 2>/dev/null | grep -E 'start as|unreachable|connected|restart|giving up|gave up' | tail -12
c=$("$M" detail --json dbapp | sed -n 's/.*"application_restart_count": *\([0-9]*\).*/\1/p')
if "$M" status dbapp | grep -q running && [ "${c:-1}" = 0 ]; then
  echo "PASS: dbapp running, 0 restarts used"
else
  echo "FAIL: dbapp not running on its first attempt (restart_count=${c:-?})"; exit 1
fi

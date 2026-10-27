#!/bin/sh
# Permission matrix for a Linux system install: what a regular user can and
# cannot do once root has initialized Morrow.
#
#   sudo sh test/e2e/escalation.sh <regular-user> <morrow-binary> <demo-api-binary>
#
# Writes to /etc/morrow, /var/lib/morrow and /var/log/morrow, so it refuses to
# run if any of them exist, and removes everything it created on exit. Meant
# for CI runners and throwaway machines.
set -u
U="${1:-}"; MORROW_SRC="${2:-}"; DEMO_SRC="${3:-}"
[ "$(id -u)" = 0 ] && [ -n "$U" ] && [ -f "$MORROW_SRC" ] && [ -f "$DEMO_SRC" ] || {
  echo "usage (as root): sh escalation.sh <regular-user> <morrow-binary> <demo-api-binary>"; exit 2; }
[ "$(id -u "$U")" != 0 ] || { echo "$U must be a regular user"; exit 2; }
for d in /etc/morrow /var/lib/morrow /var/log/morrow; do
  [ -e "$d" ] && { echo "refusing: $d already exists"; exit 2; }
done

BIN=/opt/morrow-e2e
SECRET="e2e-$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"
cleanup() {
  "$BIN/morrow" stop api >/dev/null 2>&1
  pkill -f "$BIN/demo-api" 2>/dev/null
  rm -rf /etc/morrow /var/lib/morrow /var/log/morrow "$BIN" /tmp/morrow-e2e-user
}
trap cleanup EXIT

# Binaries go where only root can write, like a real install.
mkdir -p "$BIN/api" && cp "$MORROW_SRC" "$BIN/morrow" && cp "$DEMO_SRC" "$BIN/demo-api" && chmod 755 "$BIN" "$BIN/morrow" "$BIN/demo-api"
printf '{}\n' > "$BIN/api/config.json"
M="$BIN/morrow"
{ $M init && $M create -w "$BIN/api" api "$BIN/demo-api" && $M create -w "$BIN/api" idle "$BIN/demo-api" \
  && $M set-env api "API_TOKEN:s=$SECRET" PLAIN=visible && $M start api; } >/dev/null || { echo "root setup failed"; exit 1; }
sleep 1
echo "layout after 'sudo morrow init':"
ls -ld /etc/morrow /etc/morrow/.morrow.key /var/lib/morrow /var/lib/morrow/morrow.db /var/log/morrow | awk '{print "  "$1, $3":"$4, $NF}'

pass=0; fail=0
# check <ok|deny> <name> <command run as $U> [pattern the output must match]
# The secret must never appear in the output of any row.
check() {
  out=$(su "$U" -s /bin/sh -c "$3" 2>&1); code=$?
  ok=1
  echo "$out" | grep -q -- "$SECRET" && ok=0
  if [ "$1" = ok ]; then [ $code -eq 0 ] || ok=0; else [ $code -ne 0 ] || ok=0; fi
  [ -n "${4:-}" ] && { echo "$out" | grep -q -- "$4" || ok=0; }
  if [ $ok = 1 ]; then pass=$((pass+1)); r=PASS; else fail=$((fail+1)); r=FAIL; fi
  printf '%s  %-4s %-40s %s\n' "$r" "$1" "$2" "$(echo "$out" | tail -1 | cut -c1-100)"
}
HINT="run it with sudo"

echo; echo "as $U:"
check ok   "list"                               "$M list" "running"
check ok   "status"                             "$M status api" "running"
check ok   "detail masks secret"                "$M detail api" 'API_TOKEN=\*\*\*\*'
check ok   "detail --json masks secret"         "$M detail api --json" '"API_TOKEN": "\*\*\*\*"'
check ok   "list-env masks secret"              "$M list-env api" 'API_TOKEN=\*\*\*\*'
check ok   "get-env unsecured"                  "$M get-env api PLAIN" "PLAIN=visible"
check deny "get-env secured"                    "$M get-env api API_TOKEN" "$HINT"
check deny "logs"                               "$M logs api -n 2" "$HINT"
check deny "create"                             "$M create x /bin/true" "$HINT"
check deny "set-env (overwrite secret)"         "$M set-env api API_TOKEN=attacker" "$HINT"
check deny "del-env"                            "$M del-env api PLAIN" "$HINT"
check deny "update (swap executable)"           "$M update api /bin/sh" "$HINT"
check deny "stop"                               "$M stop api" "$HINT"
check deny "restart"                            "$M restart api" "$HINT"
check deny "start (stopped app)"                "$M start idle" "$HINT"
# Match the process's own command line (pgrep -f would also match this shell).
check deny "...and no untracked process left"  "ps -u \$(id -u) -o args= | grep -q '^$BIN/demo-api'"
check deny "delete -f"                          "$M delete api -f" "$HINT"
check deny "init"                               "$M init" "sudo"
check deny "init --force (wipe secrets)"        "$M init --force" "sudo"
check deny "read key file"                      "cat /etc/morrow/.morrow.key"
check deny "list key dir"                       "ls /etc/morrow"
check deny "overwrite database"                 "cp /dev/null /var/lib/morrow/morrow.db"
check deny "plant file next to database"       "touch /var/lib/morrow/morrow.db-journal"
check deny "write into logs dir"                "touch /var/log/morrow/x.log"
check deny "replace morrow binary"              "cp /bin/true $M"
check deny "own key via MORROW_KEY_PATH"        "head -c 32 /dev/urandom > /tmp/morrow-e2e-k && MORROW_KEY_PATH=/tmp/morrow-e2e-k $M get-env api API_TOKEN" "cannot decrypt"
check deny "copied database, no key"            "mkdir -p /tmp/morrow-e2e-user && cp /var/lib/morrow/morrow.db /tmp/morrow-e2e-user/ && MORROW_HOME=/tmp/morrow-e2e-user $M get-env api API_TOKEN" "cannot decrypt"
check ok   "own per-user install"               "d=\$(mktemp -d) && MORROW_HOME=\$d $M init && MORROW_HOME=\$d $M list; r=\$?; rm -rf \$d; exit \$r" "No applications"

echo; echo "as root afterwards (system state must be unchanged):"
if $M status api | grep -q running && $M get-env api API_TOKEN | grep -q -- "$SECRET" \
   && $M get-env api PLAIN | grep -q visible && [ "$($M list | grep -c '| ')" -ge 3 ]; then
  pass=$((pass+1)); echo "PASS  api running, secret and variables intact, apps intact"
else
  fail=$((fail+1)); echo "FAIL  system state changed"; $M list; $M list-env api
fi
echo; echo "result: $pass passed, $fail failed"
[ $fail -eq 0 ]

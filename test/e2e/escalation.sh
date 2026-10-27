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
getent group morrow-deploy >/dev/null && { echo "refusing: group morrow-deploy already exists"; exit 2; }

BIN=/opt/morrow-e2e
rand() { head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n'; }
SECRET="e2e-$(rand)"
# The value the deployer submits through the inbox; it must never be readable either.
NEWSECRET="e2einbox$(rand)"
cleanup() {
  "$BIN/morrow" stop api >/dev/null 2>&1
  pkill -f "$BIN/demo-api" 2>/dev/null
  rm -rf /etc/morrow /var/lib/morrow /var/log/morrow "$BIN" /tmp/morrow-e2e-user /tmp/morrow-e2e-link /tmp/morrow-e2e-key.bak
  groupdel morrow-deploy 2>/dev/null
}
trap cleanup EXIT

# Binaries go where only root can write, like a real install.
mkdir -p "$BIN/api" && cp "$MORROW_SRC" "$BIN/morrow" && cp "$DEMO_SRC" "$BIN/demo-api" && chmod 755 "$BIN" "$BIN/morrow" "$BIN/demo-api"
printf '{}\n' > "$BIN/api/config.json"
M="$BIN/morrow"
{ $M init --enable-inbox --inbox-hook=false && $M create -w "$BIN/api" api "$BIN/demo-api" && $M create -w "$BIN/api" idle "$BIN/demo-api" \
  && $M set-env api "API_TOKEN:s=$SECRET" PLAIN=visible && $M start api \
  && usermod -aG morrow-deploy "$U" && $M grant "$U" --app api --vars API_TOKEN \
  && $M grant nobody --app api --vars API_TOKEN; } >/dev/null || { echo "root setup failed"; exit 1; }
sleep 1
echo "layout after 'sudo morrow init --enable-inbox':"
ls -ld /etc/morrow /etc/morrow/.morrow.key /etc/morrow/.morrow-inbox.key /var/lib/morrow /var/lib/morrow/morrow.db \
  /var/lib/morrow/inbox.pub /var/lib/morrow/inbox /var/log/morrow | awk '{print "  "$1, $3":"$4, $NF}'

pass=0; fail=0
# check <ok|deny> <name> <command run as $U> [pattern the output must match]
# Neither secret may ever appear in the output of any row.
check() {
  out=$(su "$U" -s /bin/sh -c "$3" 2>&1); code=$?
  ok=1
  echo "$out" | grep -q -e "$SECRET" -e "$NEWSECRET" && ok=0
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

# --- Write-only secrets through the inbox -------------------------------------
INBOX=/var/lib/morrow/inbox
echo; echo "inbox, as $U (member of morrow-deploy, granted API_TOKEN of api):"
check ok   "submit granted secret (from own env)"  "API_TOKEN=$NEWSECRET $M submit-env api API_TOKEN --restart" "Submitted 1"
check deny "list the inbox"                        "ls $INBOX"
check deny "read submissions back"                 "cat $INBOX/*"
check deny "read inbox private key"                "cat /etc/morrow/.morrow-inbox.key"
check deny "submit a variable not granted"         "$M submit-env api DB_PASS=abcdefgh12345678" "no grant"
check deny "submit a denied name"                  "$M submit-env api LD_PRELOAD=/tmp/x.so" "cannot be granted"
check deny "submit a value outside the format"     "$M submit-env api 'API_TOKEN=\$(id)'" "token format"
check deny "submit for another app"                "$M submit-env idle API_TOKEN=abcdefgh12345678" "no grant"
check deny "apply as non-root"                     "$M apply" "$HINT"
check ok   "plant a junk file (apply must reject)" "printf junk > $INBOX/0123456789abcdef0123456789abcdef.sealed"
# Two names for one file (link count 2); apply must reject both.
check ok   "plant hard links (apply must reject)"  "printf x > $INBOX/fedcba9876543210fedcba9876543210.sealed && ln $INBOX/fedcba9876543210fedcba9876543210.sealed $INBOX/00112233445566778899aabbccddeeff.sealed"
# nobody holds a grant but is not in morrow-deploy, so the inbox itself must refuse it.
out=$(su nobody -s /bin/sh -c "API_TOKEN=abcdefgh12345678 $M submit-env api API_TOKEN" 2>&1); code=$?
if [ $code -ne 0 ] && echo "$out" | grep -q "morrow-deploy"; then pass=$((pass+1)); r=PASS; else fail=$((fail+1)); r=FAIL; fi
printf '%s  %-4s %-40s %s\n' "$r" deny "granted non-member cannot write inbox" "$(echo "$out" | tail -1 | cut -c1-100)"

echo; echo "as root: morrow apply"
applied=$($M apply 2>&1); echo "  $applied" | head -3
if echo "$applied" | grep -q "Applied 1, rejected 3" && $M get-env api API_TOKEN | grep -q -- "$NEWSECRET" \
   && $M status api | grep -q running && [ -z "$(ls -A $INBOX)" ]; then
  pass=$((pass+1)); echo "PASS  submission applied, junk and hard links rejected, app restarted, inbox emptied"
else
  fail=$((fail+1)); echo "FAIL  apply result unexpected"; $M inbox
fi

echo; echo "as $U after apply:"
check deny "read the applied secret"               "$M get-env api API_TOKEN" "$HINT"
check ok   "see outcomes in 'morrow inbox'"        "$M inbox" "applied"
check ok   "outcomes show rejections, no values"   "$M inbox" "rejected"

# --- Uninstall -----------------------------------------------------------------
# pass_if <name> <shell condition run as root>
pass_if() {
  if sh -c "$2"; then pass=$((pass+1)); echo "PASS  $1"; else fail=$((fail+1)); echo "FAIL  $1"; fi
}
demo_running="ps -eo args= | grep -q '^$BIN/demo-api'"
echo; echo "uninstall, as $U:"
check deny "uninstall"                             "$M uninstall --yes" "requires elevation"
check deny "uninstall --purge"                     "$M uninstall --purge --yes" "requires elevation"

echo; echo "uninstall, as root:"
$M uninstall --dry-run >/dev/null
pass_if "--dry-run changes nothing" "$demo_running && [ -d $INBOX ] && [ -f /etc/morrow/.morrow.key ] && getent group morrow-deploy >/dev/null"
printf 'n\n' | $M uninstall >/dev/null
pass_if "answering 'n' changes nothing" "$demo_running && [ -d $INBOX ]"
$M uninstall --yes >/dev/null
pass_if "uninstall stops apps, removes inbox and group" "! $demo_running && [ ! -e $INBOX ] && [ ! -e /var/lib/morrow/inbox.pub ] && [ ! -e /etc/morrow/.morrow-inbox.key ] && ! getent group morrow-deploy >/dev/null"
pass_if "...and keeps database, key and logs" "[ -f /var/lib/morrow/morrow.db ] && [ -f /etc/morrow/.morrow.key ] && [ -d /var/log/morrow ]"
$M init >/dev/null
pass_if "reinstall resumes with apps and secrets" "$M list | grep -q '| api ' && $M get-env api API_TOKEN | grep -q -- '$NEWSECRET'"
$M start api >/dev/null
printf 'yes\n' | $M uninstall --purge >/dev/null
pass_if "purge refuses anything but the word 'purge'" "[ -f /etc/morrow/.morrow.key ] && $demo_running"
$M uninstall --purge --yes --backup-key /tmp/morrow-e2e-key.bak >/dev/null
pass_if "purge removes all data, stops apps" "[ ! -e /etc/morrow ] && [ ! -e /var/lib/morrow ] && [ ! -e /var/log/morrow ] && ! $demo_running"
pass_if "...backs up the key, keeps the binary" "[ \$(wc -c < /tmp/morrow-e2e-key.bak) -eq 32 ] && [ -x $M ]"
echo; echo "result: $pass passed, $fail failed"
[ $fail -eq 0 ]

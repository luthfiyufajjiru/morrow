#!/bin/sh
# Records the README demo scenes on a real system install. Run as root on a
# throwaway Linux machine (or WSL):
#   sh capture.sh
# It creates /etc/morrow, /var/lib/morrow, /var/log/morrow, the morrow-deploy
# group and a demo user "ci", and removes all of them at the end. It refuses to
# run if any of them already exist.
#
# Lines shown with "sudo" run as root (this script already is root, so the
# prefix itself is not executed); lines without it run as the regular user
# "ci", so the recording shows the real permission split. The demo binary is
# put first on PATH, so commands appear as plain 'morrow'.
# Output: sceneN.txt with "@@CMD <command>" followed by that command's raw output.
SRC="$(dirname "$(readlink -f "$0")")"
DEMO_USER=ci
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
for d in /etc/morrow /var/lib/morrow /var/log/morrow; do [ -e "$d" ] && { echo "refusing: $d exists"; exit 1; }; done
id "$DEMO_USER" >/dev/null 2>&1 && { echo "refusing: user $DEMO_USER exists"; exit 1; }
getent group morrow-deploy >/dev/null && { echo "refusing: group morrow-deploy exists"; exit 1; }

cleanup() {
  /tmp/demo/bin/morrow stop api >/dev/null 2>&1
  pkill -f /tmp/demo/bin/demo-api 2>/dev/null
  # su sessions under systemd start a per-user manager that keeps the account busy.
  loginctl terminate-user "$DEMO_USER" 2>/dev/null
  pkill -u "$DEMO_USER" 2>/dev/null; sleep 1
  userdel "$DEMO_USER" 2>/dev/null
  groupdel morrow-deploy 2>/dev/null
  rm -rf /etc/morrow /var/lib/morrow /var/log/morrow /tmp/demo
}
trap cleanup EXIT

useradd -M -s /bin/sh "$DEMO_USER"
rm -rf /tmp/demo; mkdir -p /tmp/demo/bin /tmp/demo/api
cp "$SRC/morrow" "$SRC/demo-api" /tmp/demo/bin/ && chmod 755 /tmp/demo /tmp/demo/bin /tmp/demo/bin/*
printf '{"name":"api","debug":false}\n' > /tmp/demo/api/config.json
chmod 755 /tmp/demo/api && chmod 644 /tmp/demo/api/config.json
cd /tmp/demo
# The pipeline's own secret store, as a CI system would expose it.
CI_API_TOKEN="rotated-by-ci-7f3a9c2e"

run() {
  echo "@@CMD $1"
  case "$1" in
    sudo\ *) PATH="/tmp/demo/bin:$PATH" sh -c "${1#sudo }" 2>&1 ;;
    *) su "$DEMO_USER" -s /bin/sh -c "PATH=/tmp/demo/bin:\$PATH; CI_API_TOKEN=$CI_API_TOKEN; cd /tmp/demo && $1" 2>&1 ;;
  esac
}
{
run "sudo morrow init"
run "sudo morrow create -w /tmp/demo/api api /tmp/demo/bin/demo-api --port 8080"
run "sudo morrow start api"
run "morrow list"
sleep 3
run "sudo morrow logs api -n 5"
} > "$SRC/scene1.txt"
{
run "sudo morrow set-env api API_TOKEN:s=s3cr3t-demo-token LOG_LEVEL=info"
run "sudo morrow restart api"
sleep 2
run "morrow detail api"
run "sudo morrow logs api -n 5"
} > "$SRC/scene2.txt"
/tmp/demo/bin/morrow detail api --json > "$SRC/detail.json" 2>&1
{
run "sudo morrow init --enable-inbox --inbox-hook=false"
run "sudo usermod -aG morrow-deploy ci"
run "sudo morrow grant ci --app api --vars API_TOKEN"
run 'API_TOKEN="$CI_API_TOKEN" morrow submit-env api API_TOKEN --restart'
run "morrow list-env api"
run "sudo morrow apply"
sleep 2
run "morrow inbox"
run "sudo morrow get-env api API_TOKEN"
} > "$SRC/scene3.txt"

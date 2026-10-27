#!/bin/sh
# Records the README demo scenes as a system install. Run as root:
#   sh capture.sh <regular-user>
# Lines shown with "sudo" run elevated exactly as written; lines without it run
# as <regular-user>, so the recording shows the real permission split.
# Output: "@@CMD <command>" followed by that command's raw output.
SRC="$(dirname "$(readlink -f "$0")")"
USER_NAME="$1"
[ "$(id -u)" = 0 ] && [ -n "$USER_NAME" ] || { echo "usage (as root): sh capture.sh <regular-user>"; exit 1; }

rm -rf /tmp/demo; mkdir -p /tmp/demo/bin /tmp/demo/api
cp "$SRC/morrow" "$SRC/demo-api" /tmp/demo/bin/ && chmod 755 /tmp/demo/bin/*
printf '{"name":"api","debug":false}\n' > /tmp/demo/api/config.json
cd /tmp/demo/bin

run() {
  echo "@@CMD $1"
  case "$1" in
    sudo\ *) sh -c "$1" 2>&1 ;;
    *) su "$USER_NAME" -s /bin/sh -c "cd /tmp/demo/bin && $1" 2>&1 ;;
  esac
}
{
run "sudo ./morrow init"
run "sudo ./morrow create -w /tmp/demo/api api /tmp/demo/bin/demo-api --port 8080"
run "sudo ./morrow start api"
run "./morrow list"
sleep 3
run "sudo ./morrow logs api -n 5"
} > "$SRC/scene1.txt"
{
run "sudo ./morrow set-env api API_TOKEN:s=s3cr3t-demo-token LOG_LEVEL=info"
run "sudo ./morrow restart api"
sleep 2
run "./morrow detail api"
run "sudo ./morrow logs api -n 5"
} > "$SRC/scene2.txt"
./morrow detail api --json > "$SRC/detail.json" 2>&1
./morrow stop api >/dev/null 2>&1

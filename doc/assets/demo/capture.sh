#!/bin/sh
# Runs each scene's commands for real and records "@@CMD <cmd>" + raw output.
SRC="$(dirname "$0")"
rm -rf /tmp/demo; mkdir -p /tmp/demo/bin /tmp/demo/api
cp "$SRC/morrow" "$SRC/demo-api" /tmp/demo/bin/ && chmod +x /tmp/demo/bin/*
printf '{"name":"api","debug":false}\n' > /tmp/demo/api/config.json
export PATH=/tmp/demo/bin:$PATH
cd /tmp/demo
run() { echo "@@CMD $1"; sh -c "$1" 2>&1; }
{
run "morrow init"
run "morrow create -w /tmp/demo/api api /tmp/demo/bin/demo-api --port 8080"
run "morrow start api"
run "morrow list"
sleep 3
run "morrow logs api -n 5"
} > "$SRC/scene1.txt"
{
run "morrow set-env api API_TOKEN:s=s3cr3t-demo-token LOG_LEVEL=info"
run "morrow restart api"
sleep 2
run "morrow detail api"
run "morrow logs api -n 5"
} > "$SRC/scene2.txt"
morrow stop api >/dev/null 2>&1

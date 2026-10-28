#!/bin/sh
# Reproduction for issue #23 "Boot ordering", run as root on a Debian 12 VM
# with systemd and PostgreSQL installed (apt-get install postgresql sudo).
# Sets up an app, run as an unprivileged account, that exits non-zero when it
# cannot reach the database; its password arrives through the inbox. Then
# installs the startup unit with or without --after (MODE=after|plain).
# Reboot, then run boot-order-check.sh.
set -eu
M=${MORROW:-/usr/local/bin/morrow}
MODE=${MODE:-after}

# A slow database start (crash recovery, a cold disk) makes the race certain.
mkdir -p /etc/systemd/system/postgresql@15-main.service.d
printf '[Service]\nExecStartPre=/bin/sleep 8\n' > /etc/systemd/system/postgresql@15-main.service.d/slow.conf
systemctl daemon-reload

id svcapp >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin svcapp
id ci >/dev/null 2>&1 || useradd --create-home --shell /bin/sh ci
PW=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
su postgres -c "psql -q -c \"DROP DATABASE IF EXISTS svcapp\" -c \"DROP ROLE IF EXISTS svcapp\" -c \"CREATE ROLE svcapp LOGIN PASSWORD '$PW'\" -c \"CREATE DATABASE svcapp OWNER svcapp\""

mkdir -p /opt/dbapp
cat > /opt/dbapp/run.sh <<'EOF'
#!/bin/sh
# Exits 3 if the database is unreachable at start, 4 if it goes away later.
c="host=127.0.0.1 dbname=svcapp user=svcapp connect_timeout=2"
echo "start as $(id -un) at $(date +%T.%N)"
psql "$c" -qtAc 'select 1' >/dev/null || { echo "db unreachable at $(date +%T.%N)"; exit 3; }
echo "connected at $(date +%T.%N)"
while sleep 5; do psql "$c" -qtAc 'select 1' >/dev/null || exit 4; done
EOF
chmod 755 /opt/dbapp/run.sh

"$M" init >/dev/null 2>&1 || true
"$M" init --enable-inbox >/dev/null
"$M" delete dbapp >/dev/null 2>&1 || true
"$M" create --user svcapp --restart on-failure --max-restarts 2 dbapp /opt/dbapp/run.sh >/dev/null

# The pipeline account sets the password through the inbox; it cannot read it.
usermod -aG morrow-deploy ci
"$M" grant ci --app dbapp --vars PGPASSWORD >/dev/null
sudo -u ci env PGPASSWORD="$PW" "$M" submit-env dbapp PGPASSWORD
for i in $(seq 1 30); do "$M" list-env dbapp 2>/dev/null | grep -q PGPASSWORD && break; sleep 1; done
"$M" list-env dbapp | grep -q '^PGPASSWORD=.*\[SECURED\]' && echo "PGPASSWORD applied from the inbox (secured)"
sudo -u ci "$M" get-env dbapp PGPASSWORD >/dev/null 2>&1 && { echo "FAIL: ci can read the secret"; exit 1; }

"$M" start dbapp
sleep 2
"$M" status dbapp
if [ "$MODE" = after ]; then "$M" startup --after postgresql.service; else "$M" startup; fi
grep -E '^(Wants|After)=' /etc/systemd/system/morrow.service

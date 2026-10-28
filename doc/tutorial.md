# Morrow tutorial and FAQ

This guide walks through everything Morrow does, from the first `init` to letting a CI/CD pipeline set secrets it can never read. Commands are shown for Linux/macOS; on Windows, run the `sudo` lines from an **Administrator terminal** without the `sudo`.

- [1. Pick an install mode](#1-pick-an-install-mode)
- [2. Run your first app](#2-run-your-first-app)
- [3. Environment variables and secrets](#3-environment-variables-and-secrets)
- [4. Who can do what](#4-who-can-do-what)
- [5. Let a pipeline set secrets it cannot read](#5-let-a-pipeline-set-secrets-it-cannot-read)
- [6. Keys, backups and recovery](#6-keys-backups-and-recovery)
- [7. Upgrading](#7-upgrading)
- [8. Uninstalling](#8-uninstalling)
- [9. Troubleshooting](#9-troubleshooting)
- [10. FAQ](#10-faq)

---

## 1. Pick an install mode

| | System install | Per-user install |
| --- | --- | --- |
| For | Servers: apps shared by the machine, managed by admins | Your own apps on a workstation, dev boxes |
| Set up with | `sudo morrow init` | `export MORROW_HOME=~/.morrow` then `morrow init` |
| Apps run as | root / Administrator (whoever starts them) | you |
| Data | `/etc/morrow` (key), `/var/lib/morrow` (database), `/var/log/morrow` (logs); Windows: `%ProgramData%\Morrow` | everything in `$MORROW_HOME` |
| Regular users | can read (`list`, `status`, `detail`, `list-env`), cannot change anything | n/a: only you |

```bash
# System install
sudo morrow init

# Per-user install
export MORROW_HOME="$HOME/.morrow"   # put this in your shell profile
morrow init
```

Running `init` again is safe: it keeps the existing key and re-applies file permissions.

**Shell completion.** Tab completes commands, flags, app names and variable names (never values). Set it up once:

```bash
# bash (Linux)
morrow completion bash | sudo tee /etc/bash_completion.d/morrow > /dev/null
# zsh
morrow completion zsh > "${fpath[1]}/_morrow"
# fish
morrow completion fish > ~/.config/fish/completions/morrow.fish
```

```powershell
# PowerShell (Windows): add to your profile
morrow completion powershell | Out-String | Invoke-Expression
```

Open a new shell afterwards. Completion works before `init` and for regular users; it only reads names from the database.

## 2. Run your first app

```bash
# Register: morrow flags (like -w) go BEFORE the app name;
# everything after the executable is passed to your app unchanged.
sudo morrow create -w /srv/api api /usr/bin/python3 main.py --port 8080

sudo morrow start api          # starts it in the background, detached from your terminal
morrow list                    # name, status, PID (no sudo needed)
morrow status api
morrow detail api              # full config; add --json for scripts
sudo morrow logs api -n 100    # last 100 lines; -f to follow
sudo morrow restart api
sudo morrow stop api
sudo morrow delete api         # add -f to stop it first if it is running
```

Change the command or working directory later with `update`:

```bash
sudo morrow update -w /srv/api-v2 api /usr/bin/python3 main.py --port 9090
sudo morrow update -w "" api /usr/bin/python3 main.py   # -w "" clears the working directory
```

**Working directory (`-w`).** Apps that read relative paths (`config.yml`, `.env`, a Python venv) need it. Without `-w`, the app inherits the directory you ran `morrow start` from, which changes from run to run.

**Logs.** Each app's stdout and stderr go to `<logs>/<app>.log`, rotated at 10 MB (3 old files kept, gzipped, 28 days). A small relay process per app does this; it also marks the app `stopped` when it exits.

**Stopping.** `morrow stop` sends SIGTERM to the app and its child processes, waits up to 10 seconds for it to exit (`--timeout 30s` to change), then kills it, and returns only once it is gone. `restart` does the same before starting it again, so the old process never overlaps the new one. On Windows, console apps cannot be asked to exit, so they are terminated right away.

**Crashes.** Each app runs as the child of a small supervisor (its log relay), which sees the real exit code, or the signal that killed it. `morrow detail` shows it as **Last Exit**. What happens next is the app's restart policy:

| `--restart` | When the app exits without `morrow stop` |
| --- | --- |
| `no` (default) | it is recorded as `exited` and stays down |
| `on-failure` | it is restarted after a non-zero exit code or a signal; exit code 0 means "done" |
| `unless-stopped` | it is restarted after any exit |

```bash
sudo morrow create --restart on-failure api /srv/api/server     # or: sudo morrow update --restart on-failure ...
sudo morrow update --max-restarts 10 api /srv/api/server        # default 5
```

Restarts wait 1s, 2s, 4s, ... (at most a minute) and give up after `--max-restarts` restarts in a row; an app that ran for at least a minute before exiting starts counting from zero again. While it waits the status is `restarting`, and `morrow stop` cancels the restart. Each restart re-reads the app's variables, so a rotated secret takes effect, and keeps the `start -e` values. Every decision is written to the app's log, e.g. `[morrow] exited unexpectedly after 3s (exit code 1); restarting in 2s (attempt 2/5)`.

**Health checks.** A restart policy only acts when the app exits. An app that hangs (deadlocked, stuck on a dead connection) keeps running. Give it a health check, and its relay restarts it when it stops answering:

```bash
sudo morrow create --health-url http://127.0.0.1:8080/healthz api /srv/api/server
sudo morrow update --health-interval 5s --health-failures 3 api /srv/api/server   # defaults: 10s and 3
sudo morrow update --health-url "" api /srv/api/server                            # remove the check
```

While the app runs, its relay sends a GET to the URL every `--health-interval`. Any answer below HTTP 400 passes. Redirects are not followed, proxy variables are ignored, and the timeout is the interval, at most 10s. The first check comes one interval after the start, so a slow-starting app has `--health-failures` x `--health-interval` to come up. After `--health-failures` failures in a row, the app is stopped like `morrow stop` (SIGTERM, then SIGKILL after 10s) and restarted with the usual backoff. This happens with any restart policy, including `no`, and counts against `--max-restarts`. Each failure and the restart are written to the app's log (`[morrow] health check failed (2/3): HTTP 503`), and the `unhealthy` hook event fires. `morrow detail` shows the check. A check does not delay `start`: it returns as soon as the app has been launched.

**Running as a dedicated account.** On a system install apps run as root/Administrator unless you say otherwise. Run each app as its own unprivileged account, so a compromised app cannot read the master key, other apps' data or anything root owns:

```bash
sudo useradd --system --no-create-home svc-api                 # once
sudo morrow create --user svc-api -w /srv/api api /srv/api/server
sudo morrow update --user "" api /srv/api/server              # back to root
```

`create` and `start` print a warning to stderr when an app would run as root/Administrator (an elevated install without `--user`); the command still succeeds.

The supervisor stays root (it writes the logs and decrypts the app's secured variables, which the app still receives), and only the app runs as `svc-api`, with that account's `HOME`/`USER`. The account needs read access to the executable and the working directory.

On **Windows**, starting a process as another account needs its password. Pass it through an environment variable, so it never appears on the command line; Morrow stores it encrypted with the master key:

```powershell
$env:SVC_PW = '...'; morrow create --user svc-api --password-env SVC_PW api C:\srv\api\server.exe; Remove-Item Env:SVC_PW
```

The account must be allowed to log on as a batch job or locally. `morrow detail` shows the account but never the password. If you change the account's password, run `morrow update --user ... --password-env ...` again.

**Alerts.** Hooks run a command or call a webhook when an app exits unexpectedly:

```bash
sudo morrow hook add api --on crash,giveup --webhook https://hooks.slack.com/services/T000/B000/XXXX
sudo morrow hook add api --on giveup --exec 'logger -t morrow "$MORROW_APP_NAME gave up (exit $MORROW_EXIT_CODE)"'
morrow hook list api              # targets are shown only to administrators
sudo morrow hook test api <id>    # fire it now with a sample event
sudo morrow hook remove api <id>
```

Events: `crash` (non-zero exit or a signal), `exit` (exit code 0), `giveup` (the restart policy stopped), `unhealthy` (the health check failed and the app is being restarted). Without `--on`, a hook gets `crash`, `giveup` and `unhealthy`. Commands get `MORROW_EVENT`, `MORROW_APP_NAME`, `MORROW_EXIT_CODE`, `MORROW_EXIT_SIGNAL`, `MORROW_RESTART_COUNT`, `MORROW_EVENT_TIME` and `MORROW_HOST`; webhooks receive the same fields as a JSON POST. Hooks run as the supervisor (root/Administrator on a system install) with a timeout, and are stored encrypted with the master key, since commands and URLs often hold tokens. The app's log records whether each hook ran, naming only the webhook's host.

**Reboots.** Run this once on a server:

```bash
sudo morrow startup            # Windows: from an Administrator terminal
morrow startup --status        # is it installed, and which apps start at boot
```

At every boot it starts the apps you started and did not stop yourself: `morrow start`/`restart` adds an app to that list, `morrow stop` removes it, while crashes, automatic restarts and `uninstall` leave it alone.

- **Linux:** a systemd unit, `morrow.service`, which also stops the apps gracefully when the machine shuts down. Restarting or stopping the unit by hand never stops apps; only a real shutdown does.
- **Windows:** a scheduled task, `Morrow\Startup`, that runs as SYSTEM shortly after startup. Apps it starts run in the services session, so they keep running when users log off. Apps you start yourself from an Administrator terminal run in your session and may stop when you log off; a reboot (or `schtasks /Run /TN Morrow\Startup` after stopping them) moves them to SYSTEM.
- `sudo morrow startup --remove` uninstalls the hook without stopping anything.

**Start after the database.** The unit waits for the network (`network-online.target`), not for local services. If an app needs one, such as PostgreSQL on the same machine, order the unit after it, or the app may start first, fail, and use up its `--max-restarts` before the database is ready:

```bash
sudo morrow startup --after postgresql.service                 # repeat --after for each service
sudo morrow startup --after postgresql.service --after redis-server.service
grep -E '^(Wants|After)=' /etc/systemd/system/morrow.service   # check
```

A bare name means a service (`--after postgresql` is `postgresql.service`). `--after` adds both `Wants=` (start that service too) and `After=` (wait until it has started), and at shutdown systemd stops the apps before that service. Running `morrow startup` again rewrites the unit, so repeat every `--after` you need. `--after` only waits for the service to report started. On Debian, `postgresql.service` reports started once the server accepts connections. For a service that reports started earlier, also have the app retry its connections or give it `--restart on-failure` with enough `--max-restarts`. Windows has no `--after`: the task starts 15 seconds after boot.

## 3. Environment variables and secrets

```bash
sudo morrow set-env api LOG_LEVEL=info PORT=8080            # plain values
sudo morrow set-env api DB_PASSWORD:s=hunter2               # :s = secured (encrypted)
sudo morrow set-env --secured api API_KEY=abc STRIPE_KEY=xyz  # --secured = all secured...
sudo morrow set-env --secured api DEBUG:u=false             # ...except :u (unsecured)

morrow list-env api            # secured values show as **** unless you can read the key
sudo morrow get-env api DB_PASSWORD
sudo morrow del-env api PORT

# One-off values for a single start; not saved
sudo morrow start api -e FEATURE_FLAG=on -e LOG_LEVEL=debug
```

Changes take effect on the next `start`/`restart`.

**How secured values are protected.** They are encrypted with AES-256-GCM using the master key. Whoever can read the key file can decrypt them: on a system install that is only root/Administrators. `detail` (table and JSON, including "Full Command") always shows `****` for secured values, even to admins, so it is safe to paste into tickets.

## 4. Who can do what

On a system install, as a regular user (not root/Administrator):

| Allowed | Denied |
| --- | --- |
| `list`, `status`, `detail` (secrets masked), `list-env` (secrets masked), `get-env` for unsecured values, `grants`, `inbox`, and `submit-env` if granted (section 5) | `get-env` of secrets, `logs`, `create`, `update`, `delete`, `start`, `stop`, `restart`, `set-env`, `del-env`, `init`, `apply`, reading the key, writing anything in the data directories |

Denied commands say so and tell you to use sudo / an Administrator terminal. Every row is checked on Linux and Windows in CI (`test/e2e/escalation.*`).

## 5. Let a pipeline set secrets it cannot read

A deploy pipeline often needs to rotate a secret (`API_TOKEN`) without being an admin, and should not be able to read secrets back. Morrow's **inbox** does this:

1. The pipeline encrypts the value with the install's **public** key and drops it in a write-only folder. It cannot decrypt it again, list the folder or read anyone's files.
2. An admin process (`morrow apply`, run automatically by a systemd unit or scheduled task) checks **who wrote the file** (the OS records the owner; it cannot be faked), that this user was **granted** that variable of that app, and that the value has the **granted format**. Then it stores the value encrypted with the master key, like `set-env --secured`, and restarts the app if asked.

### Admin: one-time setup

```bash
# 1. Enable the inbox (creates the folder, a key pair, the 'morrow-deploy' group
#    and the auto-apply hook). On Windows the group is "Morrow Deployers".
sudo morrow init --enable-inbox

# 2. Put the pipeline's account in the group (it must log in again afterwards)
sudo usermod -aG morrow-deploy ci                     # Windows: net localgroup "Morrow Deployers" ci /add

# 3. Grant exactly the variables it may set, per app, with a value format
sudo morrow grant ci --app api --vars API_TOKEN,STRIPE_KEY            # format: token (default)
sudo morrow grant ci --app api --vars LOG_LEVEL --format 'enum:info|warn|error'
sudo morrow grant ci --app api --vars UPSTREAM_URL --format url

morrow grants                                        # who may set what
sudo morrow revoke ci --app api --vars STRIPE_KEY    # or without --vars: everything for that app
```

**Auto-apply.** On Linux with systemd, `init --enable-inbox` installs `morrow-inbox.path`, which runs `morrow apply` as soon as a file arrives. On Windows it creates the scheduled task `Morrow\Inbox`, which runs every minute as SYSTEM. Elsewhere (or with `--inbox-hook=false`) run `sudo morrow apply` yourself, from cron, or from launchd.

### Pipeline: submitting

```bash
# Read the value from the pipeline's own environment (bare KEY): it never appears
# in the process list or shell history.
API_TOKEN="$CI_API_TOKEN" morrow submit-env api API_TOKEN --restart

# KEY=VALUE also works, but the value is visible to `ps` while it runs.
morrow submit-env api LOG_LEVEL=warn

# See what happened (never shows values)
morrow inbox
```

`submit-env` checks the grant and format right away, so mistakes fail in the pipeline instead of later. `--restart` restarts the app after the values are applied, if it is running.

### What is checked, and what is not

| Check | Blocks |
| --- | --- |
| Grant (name allowlist, per app) | any variable or app not explicitly granted |
| Built-in name denylist, even if granted | `PATH`, `LD_*`, `DYLD_*`, `NODE_OPTIONS`, `PYTHON*`, `PERL*`, `RUBY*`, `JAVA_TOOL_OPTIONS`, `_JAVA_OPTIONS`, `BASH_ENV`, `IFS`, `GODEBUG`, `DOTNET_STARTUP_HOOKS`, `COMSPEC`, `PATHEXT`, `MORROW_*`, ... |
| Value format | `token` (default): 8-512 chars of `A-Z a-z 0-9 . _ ~ + / = -`, so no spaces, quotes or `$(...)`. Also `int`, `bool`, `url` (https, no embedded credentials), `enum:a\|b`, `regex:<pattern>` (whole value must match), `any` |
| Every value | no control characters or newlines, valid UTF-8, at most 4 KiB |
| Every file | must be a plain file (no symlinks or hard links), at most 64 KiB, sealed for this install, unchanged, and not seen before (no replays) |

> **Limit.** Apps on a system install run as root/Administrator. These checks stop the known ways an environment variable makes programs run other code, but Morrow cannot know what *your* app does with a value. If it treats a granted variable as a file path to load or a command to run, a malicious value could still run as root. Grant only data-like variables (tokens, passwords, URLs, flags) and use the strictest format that fits. Run apps that accept submitted variables with `--user` (section 2) so such a value runs as that unprivileged account, not as root.

## 6. Keys, backups and recovery

| File | What it protects | Back it up? |
| --- | --- | --- |
| `/etc/morrow/.morrow.key` (Windows: `%ProgramData%\Morrow\.morrow.key`) | all secured values | **Yes.** Without it, secured values are gone for good |
| `/etc/morrow/.morrow-inbox.key` | inbox submissions waiting to be applied | Optional; `init --enable-inbox` makes a new pair if it is lost (pending submissions are then rejected) |
| `/var/lib/morrow/morrow.db` | apps, variables, grants | Yes |

Only `morrow init` creates the master key. If it goes missing:

- `set-env --secured` fails instead of quietly creating a new key.
- `init` refuses to create a new key while secured variables exist, and lists them. **Restore the key file** to keep them, or run `sudo morrow init --force` to create a new key and delete those variables (it prints which ones to set again).

## 7. Upgrading

Replace the binary and run `sudo morrow init` once. Database migrations run automatically on every command. Installs from before Morrow kept data in system folders (data next to the binary, e.g. `/usr/local/bin/morrow.db`) keep working from the old location until that `init`, which moves the database and key to the new folders and copies the logs. Restart running apps afterwards so they write to the new log folder, then delete the old `logs/`.

## 8. Uninstalling

```bash
sudo morrow uninstall --dry-run      # show what would happen, change nothing
sudo morrow uninstall                # stop apps, remove the inbox; keep data
sudo morrow uninstall --purge --backup-key ~/morrow.key.bak   # also delete data
```

| | `uninstall` | `uninstall --purge` |
| --- | --- | --- |
| Stop every running app and its log relay | yes | yes |
| Remove the boot hook (`morrow startup`), the inbox hook, inbox folder, inbox keys and deployers group | yes | yes |
| Delete the database, master key and logs | no: reinstall and `morrow init` resume with your apps and secrets | yes |
| Delete the morrow binary | no: it prints the path to delete, or uninstall the MSI/package | no |

- It asks first: `y` for a plain uninstall; for `--purge` you must type `purge`, after it lists the secured variables that become unrecoverable. `--yes` skips the question (scripts); with no input it aborts.
- `--backup-key <file>` copies the master key to a new file before purging. If the copy fails, nothing is deleted.
- It only deletes Morrow's own files. A folder that may hold other things (`MORROW_HOME`, or the binary's folder for an old install) is removed only if it ends up empty.
- If any app cannot be stopped, nothing is removed. It also refuses while `morrow apply` is running.
- Regular users cannot uninstall a system install. In per-user mode (`MORROW_HOME`) it needs no sudo and only touches that folder.

## 9. Troubleshooting

| Message | Meaning / fix |
| --- | --- |
| `... requires elevation: run it with sudo ... or from an Administrator terminal` | You ran `init` on a system install without elevation. Use sudo, or set `MORROW_HOME` for a per-user install |
| `this needs more access to Morrow's files than you have` | The command changes something (or reads a secret) on a system install. Use sudo / an Administrator terminal |
| `Morrow is not initialized. Please run 'morrow init' first.` | No database for the current mode. Check `MORROW_HOME`; system installs need `sudo morrow init` |
| `cannot decrypt secured variable X: ...` | You cannot read the master key (not root/Administrator), or it was replaced. See section 6 |
| `master key not found at ...` | The key file is missing. Restore it, or see section 6 |
| `application X is already running with PID N` | It really is running. If the process died, Morrow notices and shows it as stopped |
| My app got `-w DIR` as an argument / the working directory was not set | Morrow flags must come before the app name; everything after the executable goes to your app: `morrow create -w DIR app exe --port 8080` |
| `... has no grant to submit X for Y` | An admin must run `sudo morrow grant <you> --app Y --vars X` |
| `you cannot write to the inbox ...` | Ask an admin to add you to `morrow-deploy` / "Morrow Deployers", then log in again |
| `the inbox is not enabled on this install` | An admin must run `sudo morrow init --enable-inbox` |
| `X can make programs run other code, so it cannot be granted` | That name is on the denylist by design. Use a different variable name in your app |
| Submission shows `rejected` in `morrow inbox` | The reason is in the last column: format, grant, replay, file type, etc. |

## 10. FAQ

**Is Morrow like PM2?** It has PM2-style commands but no resident daemon and no Node.js. It is a single Go binary; each command runs and exits, and only a small log relay per app keeps running. With `--restart unless-stopped` its log relay restarts crashed apps with backoff. `morrow startup` starts apps at boot. It does not run apps in cluster mode.

**Do I need sudo for everything?** On a system install, for anything that changes state or reveals a secret. Reading status does not need it. In per-user mode you never need it.

**Can a regular user see my secrets?** No. They see `****`. The key is readable only by root/Administrators, and the CI permission tests try to read it, copy the database, plant files and use their own key; all of that fails.

**Can Morrow tell me when an app crashes?** Yes: `morrow hook add` runs a command or calls a webhook (Slack, PagerDuty, your own endpoint) on `crash`, `exit`, `giveup` or `unhealthy`. See [section 2](#2-run-your-first-app).

**Can my CI/CD pipeline set secrets without being admin?** Yes, with the inbox (section 5). It can only set the variables you granted, in the format you chose, and cannot read any secret, including the ones it submitted.

**Why can't I just give the pipeline `sudo morrow set-env`?** `set-env` takes several `KEY=VALUE` pairs, so a sudo rule with a wildcard lets it set `LD_PRELOAD` or `NODE_OPTIONS` and run code as root. The inbox only accepts the granted names and formats.

**Why can't the pipeline use its own per-user install?** It can, if its apps should run as the pipeline user. But then the pipeline owns that install's key and can read every secret in it.

**Does `detail` ever show secrets?** No. It always masks secured values, including in `--json` and "Full Command". Use `sudo morrow get-env` to see one value.

**Where do my app's logs go and who can read them?** `<logs>/<app>.log` (`/var/log/morrow` or `%ProgramData%\Morrow\logs`). Only root/Administrators, because apps sometimes print secrets.

**What happens after a reboot?** With `sudo morrow startup` installed, the apps you had started (and not stopped) start again automatically. Without it, `morrow list` shows them as `exited` until you start them.

**What if an app's relay is killed?** Each app runs under a small relay process that logs its output, records its exit and restarts it. If the relay is killed (`kill -9`, the OOM killer), on Linux the kernel sends the app SIGTERM at once, so it does not keep running unsupervised. The next `morrow` command that looks at the app (`status`, `list`, `start`, the boot hook) records it as `exited`, writes "the supervising relay died" to its log, and stops anything still left of it, for example a child process, or an app that ignores SIGTERM (SIGKILL after 5 seconds). Run `morrow start` to start it again; it stays wanted, so the next boot starts it too. Restart policies and alert hooks cannot run while there is no relay. Each app has at most one relay, so two `start`s at once never run two copies. On Windows there is no SIGTERM step: the app is stopped by the next command that looks at it.

**Does the inbox work in per-user mode?** No, there is no one to protect secrets from there. Use `set-env`.

**On Windows, who runs apps that `morrow apply --restart` restarts?** The scheduled task runs as SYSTEM, so apps it restarts run as SYSTEM. Apps you restart from an Administrator terminal run as you.

**How do I remove the inbox, or Morrow entirely?** `sudo morrow uninstall` stops the apps and removes the inbox (hook, folder, keys, group) but keeps your data; add `--purge` to delete the data too (section 8). Grants stay in the database, so `sudo morrow init --enable-inbox` brings the inbox back as it was.

**How do I move Morrow to another server?** Copy the database and the master key (keep the key private), install the binary, then run `sudo morrow init` on the new server: it keeps an existing key and re-applies its permissions.

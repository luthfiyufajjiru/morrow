# Morrow: Simple Cross Platform Process Manager

> New here? The **[tutorial and FAQ](doc/tutorial.md)** covers every command, the permission model, pipelines and troubleshooting.

## Getting Started

### 1. Building from Source
This project uses a unified Make-based build system that compiles binaries for multiple platforms (Windows and Linux) and builds WiX installers (`.msi`).

**Requirements:**
- Go (1.27.3+)
- Make (`GNU Make`)
- .NET SDK (for building WiX installers via `dotnet build`)
- PowerShell (used internally by Makefile for cross-platform scripts on Windows)

**Building:**
```bash
# Build everything (Go binaries and MSI installers)
make all

# Build only Go binaries
make build-go

# Clean build artifacts
make clean
```
All build outputs are output flatly into the `/dist/v<version>/` directory.
Each file is distinctly named using the `morrow-v<version>_<platform>` schema.

**Supported Targets:**
- **Windows**: x64, x86, ARM64 (plus `.msi` installers)
- **Linux**: amd64 (x86_64), arm64 (aarch64), arm (v7/32-bit)

**Versioning:**
The project uses a single source of truth for its version: the `build.xml` file.
```bash
# Update the project version automatically
make set-version V=1.1.0

# Then rebuild to apply the new version to directories, WiX files, and binaries
make all
```

### 2. Initialization
Morrow requires an initial setup to create its database and encryption keys.

**System install** must be initialized elevated, so the database and key are owned by root/Administrators:
```bash
sudo morrow init        # Linux/macOS
morrow init             # Windows: from an Administrator terminal
```
Afterwards, regular users can still run read-only commands (`list`, `status`, `detail`, `list-env`), while changes (`create`, `start`, `stop`, `set-env`, ...) and secured values require sudo/Administrator.

Its data lives outside the binary's directory, so upgrading or reinstalling the binary never touches it:

| | Linux/macOS | Windows |
| --- | --- | --- |
| Master key | `/etc/morrow/.morrow.key` (dir `0700`, file `0600`, root) | `%ProgramData%\Morrow\.morrow.key` (SYSTEM + Administrators only) |
| Database | `/var/lib/morrow/morrow.db` | `%ProgramData%\Morrow\morrow.db` |
| Logs | `/var/log/morrow/` | `%ProgramData%\Morrow\logs\` |

`init` creates these directories. Only the database directory is readable by regular users (`/var/lib/morrow` is `0755`; on Windows `%ProgramData%\Morrow` gets an explicit ACL with full control for SYSTEM and Administrators and read for Users), so read-only commands work without elevation. The logs directory is root/Administrators only (`/var/log/morrow` is `0700`; `%ProgramData%\Morrow\logs` has no Users entry), because apps may print secrets.

**Per-user install** needs no elevation: point `MORROW_HOME` at a directory you own. The database, key and `logs/` all live there, and `init` creates it if needed.
```bash
export MORROW_HOME="$HOME/.morrow"
morrow init
```

`MORROW_KEY_PATH` overrides the key location in either mode.

Re-running `init` on an existing install is safe: it keeps the key and re-applies its access restrictions.

**Upgrading from an older system install** (data next to the binary, e.g. `/usr/local/bin/morrow.db` or `C:\Program Files\Morrow\morrow.db`): the new binary keeps using the old location and prints a one-line note until you run `sudo morrow init` (Windows: `morrow init` as Administrator) once. That moves the database and key to the new location (re-applying the key restrictions) and copies the logs. Apps that are running keep writing to the old `logs/` until restarted (`morrow restart <app>`); delete the old `logs/` afterwards. If a different key already exists at the new location, `init` stops without changing anything.

### 3. Basic Usage
![morrow quick start: init, create with a working directory, start, list and logs](doc/assets/quickstart.gif)

Register a new application and start it. On a system install, commands that change state need `sudo` (Windows: an Administrator terminal); read-only commands don't. In per-user mode (`MORROW_HOME`), drop the `sudo`.
```bash
# Create the app (Morrow flags such as -w go before the app name;
# everything after the executable is passed to the app as-is)
sudo morrow create -w /path/to/workdir my-app /path/to/executable --port 8080

# Start it
sudo morrow start my-app

# Check status (read-only, no sudo needed)
morrow list
```

### 4. Shell completion
Tab completes commands, flags, app names and variable names (never values). Set it up once:

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

## Command List
| Descriptions | Commands |
| --- | --- |
| Initialize Morrow (sudo / Administrator, unless `MORROW_HOME` is set) | `morrow init [--force] [--enable-inbox]` |
| Set environment variable | `morrow set-env <app-name> <key=value> [key2=value2]...` |
| Get environment variable | `morrow get-env <app-name> <env-name>` |
| Delete environment variable | `morrow del-env <app-name> <env-name>` |
| List environment variables | `morrow list-env <app-name>` |
| List All Applications | `morrow list` |
| Create Application | `morrow create [-w <workdir>] [--restart no\|unless-stopped] [--max-restarts N] <app-name> <exec-path> <args...>` |
| Delete Application | `morrow delete <app-name>` |
| Start Application | `morrow start <app-name> [-e KEY=VALUE...]` |
| Stop Application (SIGTERM, wait, then kill) | `morrow stop [--timeout 10s] <app-name>` |
| Restart Application | `morrow restart [--timeout 10s] <app-name>` |
| Update Application | `morrow update [-w <workdir>] [--restart ...] [--max-restarts N] <app-name> <exec-path> <args...>` |
| Get Application Status | `morrow status <app-name>` |
| Get Application Detail | `morrow detail <app-name>` |
| Get Application Logs | `morrow logs <app-name>` |
| Grant a user write-only secrets (admin) | `morrow grant <user> --app <app> --vars <NAME,...> [--format token]` |
| Revoke grants (admin) | `morrow revoke <user> --app <app> [--vars <NAME,...>]` |
| List grants | `morrow grants` |
| Submit write-only secrets (no admin, needs a grant) | `morrow submit-env <app> <KEY[=VALUE]>... [--restart]` |
| Store submitted secrets (admin; normally automatic) | `morrow apply` |
| Show submission outcomes (never values) | `morrow inbox` |
| Shell completion script (bash, zsh, fish, powershell) | `morrow completion <shell>` |
| Start apps at boot (systemd unit / Windows scheduled task as SYSTEM) (admin) | `morrow startup [--status] [--remove]` |
| Uninstall: stop apps, remove the inbox, keep data; `--purge` deletes data too (admin) | `morrow uninstall [--dry-run] [--purge] [--backup-key <file>] [--yes]` |

## Detail Application Example
By default, this command outputs a human-readable table. Use the `--json` flag for programmatic integration. Secured variables are always shown as `****` here; the real value is only passed to the process.

![morrow secured env vars: set-env with a secured value, restart, detail shows it masked, logs show the app received it](doc/assets/secured-env.gif)

The outputs below are real, captured on Linux (system install; `detail` run as a regular user, without sudo) after:
```bash
sudo morrow create -w /tmp/demo/api api /tmp/demo/bin/demo-api --port 8080
sudo morrow set-env api API_TOKEN:s=s3cr3t-demo-token LOG_LEVEL=info
sudo morrow start api
```

### Default Terminal Table
**Command:**
```bash
morrow detail api
```

**Result:**
```text
+----------------------------+------------------------------------------+
| Field                      | Value                                    |
+----------------------------+------------------------------------------+
| Application ID             | 33abccf8-b237-400d-865e-1b214d1e849b     |
| Application Name           | api                                      |
| Executable Path            | /tmp/demo/bin/demo-api                   |
| Arguments                  | --port 8080                              |
| Working Directory          | /tmp/demo/api                            |
| Full Command               | API_TOKEN=**** LOG_LEVEL=info /tmp/demo/bin/demo-api --port 8080 |
| Status                     | running                                  |
| PID                        | 298                                      |
| Creation Time              | 2026-10-27T09:57:41Z                     |
| Update Time                | 2026-10-27T09:57:44Z                     |
| Status Time                | 2026-10-27T09:57:44Z                     |
| Last Run Time              | 2026-10-27T09:57:44Z                     |
+----------------------------+------------------------------------------+
```

### JSON Output
**Command:**
```bash
morrow detail api --json
```

**Result:**
```json
{
  "application_id": "33abccf8-b237-400d-865e-1b214d1e849b",
  "application_name": "api",
  "application_executable_path": "/tmp/demo/bin/demo-api",
  "application_arguments": [
    "--port",
    "8080"
  ],
  "application_working_directory": "/tmp/demo/api",
  "application_environment_variables": {
    "API_TOKEN": "****",
    "LOG_LEVEL": "info"
  },
  "application_status": "running",
  "application_pid": 298,
  "application_creation_time": "2026-10-27T09:57:41Z",
  "application_update_time": "2026-10-27T09:57:44Z",
  "application_status_time": "2026-10-27T09:57:44Z",
  "application_last_run_time": "2026-10-27T09:57:44Z",
  "application_command_line": "API_TOKEN=**** LOG_LEVEL=info /tmp/demo/bin/demo-api --port 8080"
}
```

## Example
On a system install, prefix state-changing commands with `sudo` (Windows: run from an Administrator terminal). In per-user mode (`MORROW_HOME`), run them as-is.

### Python Application
```bash
# Run from the project folder so relative paths (config, .env, venv assets) resolve
morrow create -w /home/user/my-app py-app /home/user/my-app/.venv/bin/python main.py --port 8080
morrow start py-app
```

### Binary Application (Go/Rust/C/C++)
```bash
# Compiled Go binary
morrow create go-app /home/user/apps/go-server --config /etc/go-server.yaml
morrow start go-app

# Rust binary with environment variables
morrow create rust-app /usr/local/bin/api-service
morrow set-env rust-app RUST_LOG=info
morrow start rust-app

# Start with inline (non-permanent) environment variables
morrow start rust-app -e DEBUG=true -e LOG_LEVEL=warn
```

### General Management
```bash
# Set variables in bulk (mix secured and unsecured with :s and :u suffixes)
morrow set-env py-app DB_HOST=localhost DB_PORT=5432 API_KEY:s=secret_val
morrow status py-app
morrow detail py-app
morrow stop py-app
morrow delete py-app
```



## Architecture
### Sidecar Relay Pattern
Morrow implements a **Sidecar Relay Pattern** for robust process management. 

When an application is started:
1. **Relay Spawn**: Morrow launches a detached "relay" process for that specific application.
2. **Process Isolation**: The relay process detaches from the parent terminal session, ensuring the application continues running after the user logs out or closes the terminal.
3. **Log Management**: The relay handles real-time logging, piping the application's `stdout` and `stderr` to rotating log files (using the `lumberjack` library) in the install's logs directory (`/var/log/morrow`, `%ProgramData%\Morrow\logs` or `$MORROW_HOME/logs`). It resolves the same paths as the CLI, so `morrow logs` reads what the relay writes.
4. **State Sync**: The relay continuously synchronizes the application's status (running, stopped, crashed) with the Morrow database.

This ensures high availability and reliable log capture without requiring a heavy daemon to be running constantly in the background.

## Access for pipelines (write-only secrets)
A non-admin account, such as a CI/CD pipeline, can set granted secrets without being able to read any secret:

![morrow write-only secrets: enable the inbox, grant ci API_TOKEN, ci submits a token it cannot read back, an admin applies it](doc/assets/pipeline-secrets.gif)

```bash
# Admin, once
sudo morrow init --enable-inbox                       # inbox, key pair, 'morrow-deploy' group, auto-apply hook
sudo usermod -aG morrow-deploy ci                     # Windows: net localgroup "Morrow Deployers" ci /add
sudo morrow grant ci --app api --vars API_TOKEN       # default format: token

# Pipeline (no sudo): the value is read from its own environment and sealed
API_TOKEN="$CI_API_TOKEN" morrow submit-env api API_TOKEN --restart
morrow inbox                                         # outcome, never the value
```

The pipeline seals each value with the install's public key and drops it into a folder it can write but not read. `morrow apply`, run by a systemd unit (Linux) or scheduled task (Windows), checks the file's owner against the grants, rejects names that make programs load code (`PATH`, `LD_*`, `NODE_OPTIONS`, ...), enforces the granted value format, and stores the value. Apps still run as root/Administrator, so grant only data-like variables with the strictest format; see the [tutorial](doc/tutorial.md#5-let-a-pipeline-set-secrets-it-cannot-read) and [ADR 0003](doc/adr/0003-write-only-secrets-inbox.md) for the full model and its limits.

## Security
Morrow supports encrypted environment variable storage using the `--secured` (or `-s`) flag. 
- **Access follows the key file**: secured values are encrypted with the master key (`/etc/morrow/.morrow.key`, `%ProgramData%\Morrow\.morrow.key`, `$MORROW_HOME/.morrow.key`, or `MORROW_KEY_PATH`). Whoever can read that file can decrypt them. `init` restricts it to its owner (`0600` on Linux/macOS; on Windows an explicit ACL for SYSTEM + Administrators, or only the current user in per-user mode), so in a system install only root/Administrators can read secured values.
- **Back up the key**: only `morrow init` creates it, and secured values cannot be recovered without it. If the key is lost while secured variables exist, `init` refuses to create a new one and lists them; restore the original key file, or run `morrow init --force` to create a new key and delete those variables.
- **Censorship**: `detail` (table and `--json`, including the Full Command) always shows secured values as `****`. `list-env` shows them as `****` when the key is not readable; `get-env` and `start` fail with a clear error in that case.
- **Mixed Bulk Support**: You can override the default security setting per variable by suffixing the key:
    - `key:s=value` -> Force Secured (encrypted)
    - `key:u=value` -> Force Unsecured (plaintext)




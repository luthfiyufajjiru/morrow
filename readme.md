# Morrow: Simple Cross Platform Process Manager

## Getting Started

### 1. Building from Source
This project uses a unified Make-based build system that compiles binaries for multiple platforms (Windows and Linux) and builds WiX installers (`.msi`).

**Requirements:**
- Go (1.25.5+)
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

**Versioning:**
The project uses a single source of truth for its version: the `build.xml` file.
```bash
# Update the project version automatically
make set-version V=1.1.0

# Then rebuild to apply the new version to directories, WiX files, and binaries
make all
```

### 2. Initialization
Morrow requires an initial setup to create its database and encryption keys:
```bash
./morrow init
```

### 3. Basic Usage
Register a new application and start it:
```bash
# Create the app
./morrow create my-app /path/to/executable arg1 arg2

# Start it
./morrow start my-app

# Check status
./morrow list
```

## Command List
| Descriptions | Commands |
| --- | --- |
| Initialize Morrow | `morrow init` |
| Set environment variable | `morrow set-env <app-name> <key=value> [key2=value2]...` |
| Get environment variable | `morrow get-env <app-name> <env-name>` |
| Delete environment variable | `morrow del-env <app-name> <env-name>` |
| List environment variables | `morrow list-env <app-name>` |
| List All Applications | `morrow list` |
| Create Application | `morrow create <app-name> <exec-path> <args...>` |
| Delete Application | `morrow delete <app-name>` |
| Start Application | `morrow start <app-name> [-e KEY=VALUE...]` |
| Stop Application | `morrow stop <app-name>` |
| Restart Application | `morrow restart <app-name>` |
| Update Application | `morrow update <app-name> <exec-path> <args...>` |
| Get Application Status | `morrow status <app-name>` |
| Get Application Detail | `morrow detail <app-name>` |
| Get Application Logs | `morrow logs <app-name>` |

## Detail Application Example
By default, this command outputs a human-readable table. Use the `--json` flag for programmatic integration.

### Default Terminal Table
**Command:**
```bash
morrow detail my-app
```

**Result:**
```text
+----------------------------+------------------------------------------+
| Field                      | Value                                    |
+----------------------------+------------------------------------------+
| Application ID             | 8745814f-30c8-4c6a-ad89-546ba1949b5e     |
| Application Name           | my-app                                   |
| Executable Path            | /usr/bin/python3                         |
| Arguments                  | /home/user/my-app/main.py --port 8080    |
| Full Command               | DB_URL=postgresql://localhost:5432 /usr/bin/python3 /home/user/my-app/main.py --port 8080 |
| Status                     | running                                  |
| PID                        | 1234                                     |
| Creation Time              | 2026-03-29T20:00:00Z                     |
| Update Time                | 2026-03-29T21:00:00Z                     |
| Status Time                | 2026-03-29T22:08:31Z                     |
| Last Run Time              | 2026-03-29T22:00:00Z                     |
+----------------------------+------------------------------------------+
```

### JSON Output
**Command:**
```bash
morrow detail my-app --json
```

**Result:**
```json
{
  "application_id": "8745814f-30c8-4c6a-ad89-546ba1949b5e",
  "application_name": "my-app",
  "application_executable_path": "/usr/bin/python3",
  "application_arguments": [
    "/home/user/my-app/main.py",
    "--port",
    "8080"
  ],
  "application_environment_variables": {
    "MORROW_APP_ID": "8745814f-30c8-4c6a-ad89-546ba1949b5e",
    "MORROW_APP_NAME": "my-app",
    "DB_HOST": "localhost",
    "DB_USER": "admin",
    "NODE_ENV": "production"
  },
  "application_status": "running",
  "application_pid": 1234,
  "application_command_line": "MORROW_APP_ID=8745814f-30c8-4c6a-ad89-546ba1949b5e MORROW_APP_NAME=my-app DB_URL=postgresql://localhost:5432 /usr/bin/python3 /home/user/my-app/main.py --port 8080",
  "application_creation_time": "2026-03-29T20:00:00Z",
  "application_update_time": "2026-03-29T21:00:00Z",
  "application_status_time": "2026-03-29T22:08:31Z",
  "application_last_run_time": "2026-03-29T22:00:00Z"
}
```

## Example
### Python Application
```bash
morrow create py-app /usr/bin/python3 /home/user/my-app/main.py
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


## Security
Morrow supports encrypted environment variable storage using the `--secured` (or `-s`) flag. 
- **Censorship**: By default, secured environment variables are censored as `****` in `list-env` and `detail-app` outputs.
- **Elevation**: Run `morrow` with `sudo` (root privileges) to bypass censorship and view plaintext values for secured variables.
- **Mixed Bulk Support**: You can override the default security setting per variable by suffixing the key:
    - `key:s=value` -> Force Secured (encrypted)
    - `key:u=value` -> Force Unsecured (plaintext)




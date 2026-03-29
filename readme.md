# Morrow: Process Manager for Linux based OS

## Command List
| Descriptions | Commands |
| --- | --- |
| Set environment variable | `morrow set-env <app-name> <env-name>=<env-value>` |
| Get environment variable | `morrow get-env <app-name> <env-name>` |
| Delete environment variable | `morrow del-env <app-name> <env-name>` |
| List environment variables | `morrow list-env <app-name>` |
| Get All Applications | `morrow list-apps` |
| Create Application | `morrow create-app <app-name> <exec-path> <args...>` |
| Delete Application | `morrow delete-app <app-name>` |
| Start Application | `morrow start-app <app-name>` |
| Stop Application | `morrow stop-app <app-name>` |
| Restart Application | `morrow restart-app <app-name>` |
| Update Application | `morrow update-app <app-name> <exec-path> <args...>` |
| Get Application Status | `morrow status-app <app-name>` |
| Get Application Detail | `morrow detail-app <app-name>` |
| Get All Applications | `morrow list-apps` |
| Get Application Logs | `morrow logs-app <app-name>` |

## Detail Application Example
By default, this command outputs a human-readable table. Use the `--json` flag for programmatic integration.

### Default Terminal Table
**Command:**
```bash
morrow detail-app my-app
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
morrow detail-app my-app --json
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
    "DB_HOST": "localhost",
    "DB_USER": "admin",
    "NODE_ENV": "production"
  },
  "application_status": "running",
  "application_pid": 1234,
  "application_creation_time": "2026-03-29T20:00:00Z",
  "application_update_time": "2026-03-29T21:00:00Z",
  "application_status_time": "2026-03-29T22:08:31Z",
  "application_last_run_time": "2026-03-29T22:00:00Z"
}
```

## Example
### Python Application
```bash
morrow create-app py-app /usr/bin/python3 /home/user/my-app/main.py
morrow start-app py-app
```

### Binary Application (Go/Rust/C/C++)
```bash
# Compiled Go binary
morrow create-app go-app /home/user/apps/go-server --config /etc/go-server.yaml
morrow start-app go-app

# Rust binary with environment variables
morrow create-app rust-app /usr/local/bin/api-service
morrow set-env rust-app RUST_LOG=info
morrow start-app rust-app
```

### General Management
```bash
morrow set-env py-app DB_HOST=localhost
morrow status-app py-app
morrow logs-app py-app
morrow stop-app py-app
morrow delete-app py-app
```




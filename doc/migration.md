# Database Schema and Migrations

This document details the database schema for the Morrow process manager, specifically for environment variable management and application metadata storage using SQLite (`morrow.db`).

## Migration Scripts

All migrations are stored in the `migration/` directory using the `golang-migrate` standard format (`<version>_<name>.<up|down>.sql`).

### [000001_initial_schema.up.sql](../migration/000001_initial_schema.up.sql)
This script initializes the core tables:
- `applications`: Stores application metadata (ID, name, executable path, arguments, status, etc.).
- `application_environment_variables`: Stores key-value pairs for environment variables associated with each application.

### [000001_initial_schema.down.sql](../migration/000001_initial_schema.down.sql)
Removes the initial schema tables.

### [000002_add_secured_to_envs.up.sql](../migration/000002_add_secured_to_envs.up.sql)
Adds the `env_is_secured` column to the `application_environment_variables` table, allowing for encrypted environment variable storage.

### [000002_add_secured_to_envs.down.sql](../migration/000002_add_secured_to_envs.down.sql)
Removes the `env_is_secured` column from the `application_environment_variables` table.

### [000003_add_working_directory.up.sql](../internal/db/migrations/000003_add_working_directory.up.sql)
Adds `application_working_directory` to `applications` (empty = inherit the caller's directory).

## Schema Details

### `applications` Table

| Column | Type | Description |
| --- | --- | --- |
| `application_id` | `TEXT` | Primary Key, unique identifier (UUID). |
| `application_name` | `TEXT` | Unique name for the application. |
| `application_executable_path` | `TEXT` | Full path to the executable binary. |
| `application_arguments` | `TEXT` | JSON-formatted string representing arguments. |
| `application_status` | `TEXT` | Current status (`running`, `stopped`, etc.). |
| `application_pid` | `INTEGER` | Process ID if running. |
| `application_creation_time` | `DATETIME` | Time application was created. |
| `application_update_time` | `DATETIME` | Time application metadata was updated. |
| `application_working_directory` | `TEXT` | Working directory the process is started in (empty = inherit). |

### `application_environment_variables` Table

| Column | Type | Description |
| --- | --- | --- |
| `application_id` | `TEXT` | Foreign Key referencing `applications`. |
| `env_name` | `TEXT` | Key for the environment variable. |
| `env_value` | `TEXT` | Value for the environment variable. |
| `env_is_secured` | `INTEGER` | Boolean (0/1) indicating if the value is encrypted. |

---

## Applying Migrations

Morrow automatically applies these migrations upon startup if the database file is newly created or missing recent schema updates. The application looks for `morrow.db` in `/var/lib/morrow` (Linux/macOS) or `%ProgramData%\Morrow` (Windows) for a system install, or in `$MORROW_HOME` for a per-user install. Older system installs kept it next to the binary; it is read from there until `morrow init` (elevated) moves it.

### 000004_add_grants_and_inbox_events
Adds two tables for write-only secret submission (see [ADR 0003](adr/0003-write-only-secrets-inbox.md)):

| Table | Columns | Purpose |
| --- | --- | --- |
| `grants` | `principal`, `application_id`, `env_name`, `format` | Which non-admin account may submit which variable of which app, and the value format it must match |
| `inbox_events` | `submission_id` (unique), `application_name`, `env_name`, `principal`, `status`, `reason`, `event_time` | Outcome of every processed submission (never the value); the unique ID rejects replays |

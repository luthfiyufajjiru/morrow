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

### `application_environment_variables` Table

| Column | Type | Description |
| --- | --- | --- |
| `application_id` | `TEXT` | Foreign Key referencing `applications`. |
| `env_name` | `TEXT` | Key for the environment variable. |
| `env_value` | `TEXT` | Value for the environment variable. |
| `env_is_secured` | `INTEGER` | Boolean (0/1) indicating if the value is encrypted. |

---

## Applying Migrations

Morrow automatically applies these migrations upon startup if the database file is newly created or missing recent schema updates. The application looks for the `morrow.db` file in the configured `sqlite` path.

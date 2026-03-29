# ADR 0001: Environment Management and Application Metadata Storage

## Status
Accepted

## Context
The "Morrow" process manager needs a way to store metadata for managed applications. This metadata includes process identifiers, executable paths, startup arguments, and crucially, environment variables. Environment management is a key feature, allowing users to persist specific configurations for each application.

We need a storage solution that is:
1. Persistent across reboots.
2. Capable of handling structured data (application record -> many environment variables).
3. Lightweight and easy to distribute.

## Decision
We will use **SQLite** as the primary storage for all application metadata, including environment variables.

### Layout
**Development (from Root):**
```text
bin/          # Contains binary files
morrow.db     # SQLite database file
```

**Distribution:**
```text
dist/morrow/<version>/
├── bin/          # Contains binary files
└── morrow.db     # SQLite database file
```

All application metadata, including name, executable paths, arguments, and environment variables, will be stored within the `morrow.db` database file.

## Consequences
- **Persistence**: Application configuration and environment variables survive system restarts.
- **ACID Compliance**: Ensuring data integrity during updates.
- **Zero Configuration**: SQLite does not require a separate database server.
- **Structured Representation**: Easier to query application details for both human-readable output and JSON integration.
- **Portability**: The entire state of the process manager can be backed up or moved by copying the `morrow.db` database file.
- **Development Overhead**: Requires introducing a database schema and a DAL (Data Access Layer) in the Go/CLI code.

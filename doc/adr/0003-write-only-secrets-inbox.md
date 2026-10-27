# ADR 0003: Write-only secrets for non-admin deployers (the inbox)

## Status
Accepted

## Context
On a system install only root/Administrators can change apps or read secured variables (ADR 0002). CI/CD pipelines often run under a non-admin account but need to rotate secrets such as `API_TOKEN`. They should be able to **set** granted secrets without being able to **read** any secret, and without gaining a way to run code as root.

Constraints:
- Morrow is daemonless: there is no privileged process to accept requests.
- Apps on a system install run as root, so any variable a deployer can set is a potential route to code execution as root (`LD_PRELOAD`, `NODE_OPTIONS`, ...).
- Must work on Linux and Windows.

Alternatives considered:
1. **Per-user install for the pipeline.** No escalation, but the pipeline owns that install's key and can read every secret in it, and its apps run as the pipeline user.
2. **`sudo morrow set-env` rule.** Linux only. Because `set-env` accepts several pairs, a wildcard rule lets the pipeline set `LD_PRELOAD` and become root.
3. **sudo + grants enforced through `SUDO_USER`.** Safe on Linux, no Windows equivalent for non-admins.
4. **Sealed inbox (chosen).** Works on both OSes without sudo.

## Decision
- `morrow init --enable-inbox` creates an X25519 key pair for NaCl anonymous sealed boxes (`golang.org/x/crypto/nacl/box`). The private key sits next to the master key with the same restrictions; the public key next to the database, readable by all.
- Deployers seal `{v, id, app, name, value, ts, restart}` with the public key and write it to an inbox folder they can create files in but not list or read: Linux `root:morrow-deploy` mode `1730`; Windows a protected DACL giving the "Morrow Deployers" group only `FILE_ADD_FILE`. They cannot decrypt their own submission.
- `morrow apply` (root/Administrator) identifies the submitter from the **file owner** recorded by the OS, which a regular user cannot set to another account. It rejects non-regular files (symlinks, reparse points, hard links), oversized files, files not sealed for this install, mismatched file names and replayed IDs, then checks the grant and format and stores the value with the master key. Outcomes, never values, are recorded in `inbox_events`.
- Grants (`grants` table) are an explicit per-app variable allowlist with a value format. A built-in name denylist (loader, interpreter and shell hooks) applies even to granted names. Universal rules reject control characters, invalid UTF-8 and values over 4 KiB.
- Without a daemon, applying is triggered by an OS hook installed by `init`: a systemd path unit (`DirectoryNotEmpty`) on Linux, a scheduled task running every minute as SYSTEM on Windows. Admins can always run `morrow apply` themselves.

## Consequences
- Pipelines can rotate granted secrets on both OSes and cannot read any secret. CI (`test/e2e/escalation.*`) proves this end to end.
- A compromised pipeline can still overwrite the secrets it was granted (integrity, not confidentiality, is delegated).
- Validation reduces but cannot eliminate code-execution risk: an app may treat a granted variable as code or a path. Documented; the complete fix is running apps as dedicated users (future work).
- On Windows, apps restarted by `apply` run as SYSTEM.
- Adds a dependency on `golang.org/x/crypto` and a database migration (`000004`).

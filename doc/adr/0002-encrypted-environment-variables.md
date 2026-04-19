# ADR 0002: Secure Storage for Environment Variables

## Status
Accepted

## Context
Morrow stores environment variables in a SQLite database. Some variables (API keys, passwords) are sensitive and should not be stored in plain text. We need a mechanism to:
1.  Protect secrets at rest in the database.
2.  Ensure only authorized users (Root/Admin) can decrypt and view secrets.
3.  Allow the process manager to decrypt secrets at runtime to inject them into child processes.

## Decision
We will use **Symmetric Encryption (AES-GCM)** tied to a **Root-Protected Master Key**.

### Key Details
- **Algorithm:** AES-256-GCM (Authenticated Encryption).
- **Key Storage:** A 32-byte random Master Key stored in a file:
    - Production: `/etc/morrow/master.key`
    - Development: `./.morrow.key`
- **Permissions:** The key file must be owned by `root` with `0600` permissions.
- **Access Control:**
    - **Encryption:** Any user can encrypt if they have access to the key.
    - **Decryption:** Only users/processes capable of reading the Master Key file (Root) can decrypt.
- **Trust Model:** We assume the Root user is trusted and the machine's primary security boundary is the OS file permissions.

### Why Symmetric over Asymmetric?
1.  **Simplicity:** Minimal overhead for a tool that runs with Root privileges anyway.
2.  **Startup Performance:** AES-GCM is hardware-accelerated on most modern CPUs.
3.  **No Runtime Daemon:** Since Morrow is a stateless CLI/Launcher, managing a single master key file is more reliable than managing PGP/RSA key pairs across different user sessions without a daemon to hold the private key in memory.

## Consequences
- **Security:** Secrets are safe if the `morrow.db` file is stolen but the Master Key is not.
- **Operational:** Users must run as `root` (or via `sudo`) to view secured variables or start applications that use them.
- **Complexity:** Adds a dependency on a key file. If the key file is lost, all secured variables in the DB become permanently unrecoverable.

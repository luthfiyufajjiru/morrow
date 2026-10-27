-- Migration 0007: exact exit information recorded by the supervising relay,
-- and alert hooks run on app exits.
ALTER TABLE applications ADD COLUMN application_last_exit_code INTEGER;
ALTER TABLE applications ADD COLUMN application_last_exit_signal TEXT NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN application_last_exit_time DATETIME;

-- Commands or webhooks run when an app exits unexpectedly. The target is
-- encrypted with the master key: commands and URLs often carry tokens.
CREATE TABLE IF NOT EXISTS hooks (
    hook_id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    hook_events TEXT NOT NULL,
    hook_kind TEXT NOT NULL,
    hook_target TEXT NOT NULL,
    hook_creation_time DATETIME NOT NULL,
    FOREIGN KEY (application_id) REFERENCES applications(application_id) ON DELETE CASCADE
);

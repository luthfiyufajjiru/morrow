-- Migration 0004: write-only secret submission through the inbox.

-- Which non-admin principal may submit which variables of which application,
-- and the value format each submission must match.
CREATE TABLE IF NOT EXISTS grants (
    principal TEXT NOT NULL,
    application_id TEXT NOT NULL,
    env_name TEXT NOT NULL,
    format TEXT NOT NULL DEFAULT 'token',
    PRIMARY KEY (principal, application_id, env_name),
    FOREIGN KEY (application_id) REFERENCES applications(application_id) ON DELETE CASCADE
);

-- Outcome of every processed submission. Never stores values. The submission
-- id is unique, so a replayed submission is rejected.
CREATE TABLE IF NOT EXISTS inbox_events (
    submission_id TEXT PRIMARY KEY,
    application_name TEXT NOT NULL,
    env_name TEXT NOT NULL,
    principal TEXT NOT NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    event_time DATETIME NOT NULL
);

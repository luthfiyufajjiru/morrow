-- Migration 0005: per-app restart policy, applied by the log relay when an app
-- exits without being stopped. 'no' keeps the previous behavior.
ALTER TABLE applications ADD COLUMN application_restart_policy TEXT NOT NULL DEFAULT 'no';
ALTER TABLE applications ADD COLUMN application_max_restarts INTEGER NOT NULL DEFAULT 5;
ALTER TABLE applications ADD COLUMN application_restart_count INTEGER NOT NULL DEFAULT 0;

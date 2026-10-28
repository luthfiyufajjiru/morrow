-- Migration 0009: optional HTTP health check, run by the supervising relay.
-- An empty URL means no health check.
ALTER TABLE applications ADD COLUMN application_health_url TEXT NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN application_health_interval INTEGER NOT NULL DEFAULT 10;
ALTER TABLE applications ADD COLUMN application_health_failures INTEGER NOT NULL DEFAULT 3;

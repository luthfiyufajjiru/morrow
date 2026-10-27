-- Migration 0008: run an app as a dedicated account instead of root/Administrator.
-- The secret (Windows only: the account's password) is encrypted with the master key.
ALTER TABLE applications ADD COLUMN application_run_as TEXT NOT NULL DEFAULT '';
ALTER TABLE applications ADD COLUMN application_run_as_secret TEXT NOT NULL DEFAULT '';

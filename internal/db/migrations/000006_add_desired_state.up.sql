-- Migration 0006: whether the user wants each app up. Set by start/restart
-- ('up') and by a user stop ('down'); automatic restarts, uninstall and system
-- shutdown leave it alone. `morrow startup` starts the 'up' apps at boot.
ALTER TABLE applications ADD COLUMN application_desired_state TEXT NOT NULL DEFAULT 'down';
UPDATE applications SET application_desired_state = 'up' WHERE application_status IN ('running', 'restarting');

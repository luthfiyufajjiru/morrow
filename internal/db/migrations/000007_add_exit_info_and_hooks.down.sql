DROP TABLE IF EXISTS hooks;
ALTER TABLE applications DROP COLUMN application_last_exit_time;
ALTER TABLE applications DROP COLUMN application_last_exit_signal;
ALTER TABLE applications DROP COLUMN application_last_exit_code;

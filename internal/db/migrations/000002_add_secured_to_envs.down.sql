-- In SQLite, deleting a column requires table recreation for versions before 3.35.0.
-- For simplicity, we just won't bother with the rollback of the column itself 
-- or use standard SQLite column removal steps (RENAME, CREATE NEW, INSERT, DROP)
ALTER TABLE application_environment_variables DROP COLUMN env_is_secured;

-- Migration 0001: Initialize applications and environment variables tables

-- Application table stores the core metadata for each managed process
CREATE TABLE IF NOT EXISTS applications (
    application_id TEXT PRIMARY KEY,
    application_name TEXT UNIQUE NOT NULL,
    application_executable_path TEXT NOT NULL,
    application_arguments TEXT NOT NULL, -- JSON formatted array of strings
    application_status TEXT NOT NULL DEFAULT 'stopped',
    application_pid INTEGER DEFAULT 0,
    application_creation_time DATETIME DEFAULT CURRENT_TIMESTAMP,
    application_update_time DATETIME DEFAULT CURRENT_TIMESTAMP,
    application_status_time DATETIME DEFAULT CURRENT_TIMESTAMP,
    application_last_run_time DATETIME
);

-- Environment variables table links to application_id
CREATE TABLE IF NOT EXISTS application_environment_variables (
    application_id TEXT NOT NULL,
    env_name TEXT NOT NULL,
    env_value TEXT NOT NULL,
    PRIMARY KEY (application_id, env_name),
    FOREIGN KEY (application_id) REFERENCES applications(application_id) ON DELETE CASCADE
);

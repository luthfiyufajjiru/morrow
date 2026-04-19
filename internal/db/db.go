package db

import (
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB(dbPath string) error {
	var err error
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

// NewMigrationDriver returns a migration driver for the current database connection.
func NewMigrationDriver() (database.Driver, error) {
	driver, err := sqlite.WithInstance(DB, &sqlite.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to create migration driver: %w", err)
	}
	return driver, nil
}

// EnsureSchema uses golang-migrate to apply migrations from a filesystem path.
func EnsureSchema(migrationPath string) error {
	driver, err := NewMigrationDriver()
	if err != nil {
		return err
	}

	// Ensure migrations path is correct for file:// source
	sourcePath := filepath.ToSlash(migrationPath)

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+sourcePath,
		"sqlite", driver)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	return nil
}

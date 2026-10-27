package env

import (
	"database/sql"
	"fmt"
	"morrow/internal/crypto"
	"morrow/internal/db"
)

type EnvVar struct {
	Name      string
	Value     string
	IsSecured bool
	// Locked is true when the variable is secured but the caller cannot read the
	// master key. Value is empty in that case.
	Locked bool
}

func appExists(name string) (string, error) {
	var appID string
	err := db.DB.QueryRow("SELECT application_id FROM applications WHERE application_name = ?", name).Scan(&appID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("application %s not found", name)
		}
		return "", err
	}
	return appID, nil
}

// SetEnv sets or updates an environment variable. If secured is true, the value is encrypted.
func SetEnv(appName string, key string, value string, isSecured bool) error {
	appID, err := appExists(appName)
	if err != nil {
		return err
	}

	finalValue := value
	if isSecured {
		encryptedValue, err := crypto.Encrypt(value)
		if err != nil {
			return fmt.Errorf("failed to encrypt value: %w", err)
		}
		finalValue = encryptedValue
	}

	_, err = db.DB.Exec(`
		INSERT INTO application_environment_variables (application_id, env_name, env_value, env_is_secured)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(application_id, env_name) DO UPDATE SET 
			env_value = Excluded.env_value,
			env_is_secured = Excluded.env_is_secured
	`, appID, key, finalValue, isSecured)
	return err
}

// GetEnv retrieves a variable and decrypts it if it's secured.
func GetEnv(appName string, key string) (string, bool, error) {
	if _, err := appExists(appName); err != nil {
		return "", false, err
	}

	var value string
	var isSecured bool
	err := db.DB.QueryRow(`
		SELECT env_value, env_is_secured FROM application_environment_variables
		JOIN applications ON application_environment_variables.application_id = applications.application_id
		WHERE applications.application_name = ? AND env_name = ?
	`, appName, key).Scan(&value, &isSecured)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, fmt.Errorf("env variable %s not found for %s", key, appName)
		}
		return "", false, err
	}

	if isSecured {
		decrypted, err := crypto.Decrypt(value)
		if err != nil {
			return "", true, fmt.Errorf("cannot decrypt secured variable %s: %w", key, err)
		}
		return decrypted, true, nil
	}

	return value, false, nil
}

// DelEnv deletes an environment variable for an application
func DelEnv(appName string, key string) error {
	appID, err := appExists(appName)
	if err != nil {
		return err
	}

	res, err := db.DB.Exec(`
		DELETE FROM application_environment_variables
		WHERE application_id = ? AND env_name = ?
	`, appID, key)
	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("env variable %s not found for %s", key, appName)
	}
	return nil
}

// ListEnv lists all variables and decrypts any that are secured.
func ListEnv(appName string) ([]EnvVar, error) {
	if _, err := appExists(appName); err != nil {
		return nil, err
	}

	rows, err := db.DB.Query(`
		SELECT env_name, env_value, env_is_secured FROM application_environment_variables
		JOIN applications ON application_environment_variables.application_id = applications.application_id
		WHERE applications.application_name = ?
	`, appName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var envs []EnvVar
	for rows.Next() {
		var e EnvVar
		var isSecuredInt int
		if err := rows.Scan(&e.Name, &e.Value, &isSecuredInt); err != nil {
			return nil, err
		}
		e.IsSecured = isSecuredInt != 0
		if e.IsSecured {
			// Access to secured values is governed by read access to the master key,
			// not by the caller's euid.
			decrypted, err := crypto.Decrypt(e.Value)
			if err != nil {
				e.Value = ""
				e.Locked = true
			} else {
				e.Value = decrypted
			}
		}
		envs = append(envs, e)
	}
	return envs, nil
}

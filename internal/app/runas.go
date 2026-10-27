package app

import (
	"fmt"
	"morrow/internal/crypto"
	"morrow/internal/db"
	"os/exec"
	"strings"
)

// child is a running app process started by the supervisor.
type child interface {
	Pid() int
	Wait() ExitInfo
}

// execChild is a process started through os/exec.
type execChild struct{ cmd *exec.Cmd }

func (c execChild) Pid() int { return c.cmd.Process.Pid }

func (c execChild) Wait() ExitInfo {
	_ = c.cmd.Wait()
	return exitInfo(c.cmd.ProcessState)
}

// SetRunAs makes the app run as account instead of the supervisor's own
// account (root/Administrator on a system install). An empty account clears
// it. On Windows starting a process as another account needs that account's
// password, stored encrypted with the master key; elsewhere none is used.
func SetRunAs(name, account, password string) error {
	account = strings.TrimSpace(account)
	secret := ""
	if account != "" {
		if err := checkRunAsAccount(account, password); err != nil {
			return err
		}
		if password != "" {
			enc, err := crypto.Encrypt(password)
			if err != nil {
				return fmt.Errorf("failed to encrypt the account password: %w", err)
			}
			secret = enc
		}
	}
	res, err := db.DB.Exec(`UPDATE applications SET application_run_as = ?, application_run_as_secret = ?, application_update_time = ?
		WHERE application_name = ?`, account, secret, now(), name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("application %s not found", name)
	}
	return nil
}

// runAsSecret returns the decrypted run-as password of the app ("" if none).
func runAsSecret(name string) (string, error) {
	var enc string
	if err := db.DB.QueryRow("SELECT application_run_as_secret FROM applications WHERE application_name = ?", name).Scan(&enc); err != nil {
		return "", err
	}
	if enc == "" {
		return "", nil
	}
	p, err := crypto.Decrypt(enc)
	if err != nil {
		return "", fmt.Errorf("cannot decrypt the run-as password: %w", err)
	}
	return p, nil
}

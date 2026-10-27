package cmd

import (
	"fmt"
	"log"
	"morrow/internal/config"
	"morrow/internal/crypto"
	"morrow/internal/db"
	"morrow/internal/env"
	"morrow/internal/inbox"
	"morrow/internal/install"
	"morrow/internal/privilege"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var InitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Morrow database and migrations",
	Run: func(cmd *cobra.Command, args []string) {
		if err := privilege.CheckInit(config.IsPerUser(), privilege.IsElevated(), config.GetDataDir()); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}

		fmt.Println("Initializing Morrow...")

		// 1. Create the data directories; for a system install also move data
		// from older installs that kept it next to the binary.
		if config.IsPerUser() {
			if err := install.PrepareUser(config.Current()); err != nil {
				log.Fatalf("Failed to create data directories: %v", privilege.Hint(err))
			}
		} else if err := setupSystem(); err != nil {
			log.Fatalf("Failed to set up system directories: %v", privilege.Hint(err))
		}

		// 2. Initialize DB and Migration
		if err := db.InitDB(config.GetDBPath()); err != nil {
			log.Fatalf("Failed to initialize database: %v", privilege.Hint(err))
		}

		if err := db.EnsureSchemaEmbedded(); err != nil {
			log.Fatalf("Failed to run embedded migrations: %v", privilege.Hint(err))
		}

		// 3. Refuse to replace a lost key while values encrypted with it exist,
		// unless --force accepts losing them.
		if crypto.MasterKeyMissing() {
			secured, err := env.ListSecured()
			if err != nil {
				log.Fatalf("Failed to check for secured variables: %v", privilege.Hint(err))
			}
			if err := checkKeyLoss(secured, forceInit, config.GetKeyPath()); err != nil {
				log.Fatalf("Error: %v", err)
			}
			if len(secured) > 0 {
				n, err := env.DeleteSecured()
				if err != nil {
					log.Fatalf("Failed to remove undecryptable secured variables: %v", privilege.Hint(err))
				}
				fmt.Printf("Removed %d secured variable(s) that the new key cannot decrypt; set them again with 'morrow set-env':\n", n)
				for _, s := range secured {
					fmt.Printf("  %s: %s\n", s.App, s.Name)
				}
			}
		}

		// 4. Initialize Master Key for Encryption
		fmt.Println("Generating master key for secured environment variables...")
		if err := crypto.InitMasterKey(); err != nil {
			log.Fatalf("Failed to initialize master key: %v", privilege.Hint(err))
		}

		// 5. Optionally enable the inbox for write-only secret submission.
		if enableInbox {
			if err := setupInbox(); err != nil {
				log.Fatalf("Failed to enable the inbox: %v", privilege.Hint(err))
			}
		}

		fmt.Println("Successfully initialized Morrow! You can now start managing applications.")
	},
}

var (
	forceInit   bool
	enableInbox bool
	inboxHook   bool
)

func init() {
	InitCmd.Flags().BoolVar(&forceInit, "force", false,
		"Create a new master key even though secured variables exist; those variables are deleted")
	InitCmd.Flags().BoolVar(&enableInbox, "enable-inbox", false,
		"Let non-admin deployers submit write-only secrets ('morrow grant', 'morrow submit-env')")
	InitCmd.Flags().BoolVar(&inboxHook, "inbox-hook", true,
		"With --enable-inbox, install a systemd unit (Linux) or scheduled task (Windows) that runs 'morrow apply'")
}

// setupInbox creates the inbox directory, its key pair and, unless disabled,
// the OS hook that applies submissions automatically.
func setupInbox() error {
	if config.IsPerUser() {
		return fmt.Errorf("the inbox is for system installs; in per-user mode (MORROW_HOME) use 'morrow set-env'")
	}
	dir := config.GetInboxDir()
	if err := privilege.PrepareInboxDir(dir); err != nil {
		return err
	}
	if err := crypto.InitInboxKeys(); err != nil {
		return err
	}
	fmt.Printf("Inbox enabled at %s. Members of the %q group can submit granted variables with 'morrow submit-env'.\n",
		dir, privilege.DeployGroup)
	if !inboxHook {
		fmt.Println("No hook installed: run 'morrow apply' as an administrator to store submissions.")
		return nil
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return err
	}
	desc, err := inbox.InstallHook(exe, dir)
	if err != nil {
		fmt.Println("Warning: automatic apply not installed:", err)
		return nil
	}
	fmt.Println("Installed", desc)
	return nil
}

// checkKeyLoss decides whether init may create a new master key when the key
// file is missing. Secured values encrypted with the old key would become
// permanently undecryptable, so that needs an explicit --force.
func checkKeyLoss(secured []env.SecuredRef, force bool, keyPath string) error {
	if len(secured) == 0 || force {
		return nil
	}
	names := make([]string, 0, len(secured))
	for _, s := range secured {
		names = append(names, s.App+": "+s.Name)
	}
	return fmt.Errorf("the master key is missing at %s, but %d secured variable(s) were encrypted with it:\n  %s\n"+
		"Restore the original key file to keep them. To create a new key and delete these variables instead, "+
		"run 'morrow init --force'", keyPath, len(secured), strings.Join(names, "\n  "))
}

func IsInitiated() bool {
	_, err := os.Stat(config.GetDBPath())
	return !os.IsNotExist(err)
}

// setupSystem creates the system install's directories and migrates a legacy
// next-to-binary install into them.
func setupSystem() error {
	layout := config.SystemLayout()
	if err := install.PrepareSystem(layout); err != nil {
		return err
	}
	legacy, ok := config.LegacyLayout()
	if !ok {
		return nil
	}
	// An overridden key stays where MORROW_KEY_PATH points.
	res, err := install.MigrateLegacy(legacy, layout, os.Getenv("MORROW_KEY_PATH") == "")
	if err != nil {
		return fmt.Errorf("failed to migrate data from %s: %w", filepath.Dir(legacy.DBPath), err)
	}
	if !res.Migrated {
		return nil
	}
	fmt.Printf("Migrated data from %s:\n  database -> %s\n  key      -> %s\n  logs     -> %s\n",
		filepath.Dir(legacy.DBPath), layout.DBPath, config.GetKeyPath(), layout.LogsDir)
	if _, err := os.Stat(legacy.LogsDir); err == nil {
		fmt.Printf("The old logs in %s were copied, not moved: apps running now keep writing there until restarted "+
			"('morrow restart <app>'). Delete that directory afterwards.\n", legacy.LogsDir)
	}
	for _, w := range res.Warnings {
		fmt.Println("Warning:", w)
	}
	return nil
}

package cmd

import (
	"fmt"
	"log"
	"morrow/internal/config"
	"morrow/internal/crypto"
	"morrow/internal/db"
	"morrow/internal/env"
	"morrow/internal/privilege"
	"os"
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

		// 1. Initialize DB and Migration
		if err := db.InitDB(config.GetDBPath()); err != nil {
			log.Fatalf("Failed to initialize database: %v", privilege.Hint(err))
		}
		
		if err := db.EnsureSchemaEmbedded(); err != nil {
			log.Fatalf("Failed to run embedded migrations: %v", privilege.Hint(err))
		}

		// 2. Refuse to replace a lost key while values encrypted with it exist,
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

		// 3. Initialize Master Key for Encryption
		fmt.Println("Generating master key for secured environment variables...")
		if err := crypto.InitMasterKey(); err != nil {
			log.Fatalf("Failed to initialize master key: %v", privilege.Hint(err))
		}
		
		fmt.Println("Successfully initialized Morrow! You can now start managing applications.")
	},
}

var forceInit bool

func init() {
	InitCmd.Flags().BoolVar(&forceInit, "force", false,
		"Create a new master key even though secured variables exist; those variables are deleted")
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

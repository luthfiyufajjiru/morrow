package cmd

import (
	"fmt"
	"log"
	"morrow/internal/config"
	"morrow/internal/crypto"
	"morrow/internal/db"
	"os"

	"github.com/spf13/cobra"
)

var InitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Morrow database and migrations",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Initializing Morrow...")
		
		// 1. Initialize DB and Migration
		if err := db.InitDB(config.GetDBPath()); err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		
		if err := db.EnsureSchemaEmbedded(); err != nil {
			log.Fatalf("Failed to run embedded migrations: %v", err)
		}

		// 2. Initialize Master Key for Encryption
		fmt.Println("Generating master key for secured environment variables...")
		if err := crypto.InitMasterKey(); err != nil {
			log.Fatalf("Failed to initialize master key: %v", err)
		}
		
		fmt.Println("Successfully initialized Morrow! You can now start managing applications.")
	},
}

func init() {
	// RootCmd is defined in root.go
}

func IsInitiated() bool {
	_, err := os.Stat(config.GetDBPath())
	return !os.IsNotExist(err)
}

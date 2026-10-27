package cmd

import (
	"fmt"
	"log"
	"morrow/internal/config"
	"morrow/internal/db"
	"morrow/internal/privilege"
	"os"

	"github.com/spf13/cobra"
)

var Version = "dev"

var RootCmd = &cobra.Command{
	Use:     "morrow",
	Short:   "Morrow is a process manager for Cross platform OS",
	Version: Version,

	PersistentPreRun: func(cmd *cobra.Command, args []string) {

		// Allow init and help commands even if not initiated
		if cmd.Name() == "init" || cmd.Name() == "help" || cmd.Name() == "_relay" {
			return
		}

		if !IsInitiated() {
			fmt.Println("Morrow is not initialized. Please run 'morrow init' first.")
			os.Exit(1)
		}

		if err := db.InitDB(config.GetDBPath()); err != nil {
			log.Fatalf("failed to open db: %v", privilege.Hint(err))
		}

		// Apply pending migrations so upgraded binaries work against older databases.
		if err := db.EnsureSchemaEmbedded(); err != nil {
			log.Fatalf("failed to migrate db: %v", privilege.Hint(err))
		}
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if db.DB != nil {
			db.DB.Close()
		}
	},
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func RegisterCommands(cmds ...*cobra.Command) {
	RootCmd.AddCommand(cmds...)
}

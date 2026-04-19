package cmd

import (
	"fmt"
	"log"
	"morrow/internal/config"
	"morrow/internal/db"
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
		if cmd.Name() == "init" || cmd.Name() == "help" {
			return
		}

		if !IsInitiated() {
			fmt.Println("Morrow is not initialized. Please run 'morrow init' first.")
			os.Exit(1)
		}

		if err := db.InitDB(config.GetDBPath()); err != nil {
			log.Fatalf("failed to open db: %v", err)
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

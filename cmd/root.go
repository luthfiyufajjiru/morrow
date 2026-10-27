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

		// Some commands run (or open the DB themselves) even if not initialized.
		if skipPreRun(cmd) {
			return
		}

		if !IsInitiated() {
			fmt.Println("Morrow is not initialized. Please run 'morrow init' first.")
			os.Exit(1)
		}

		if config.UsingLegacy() {
			fmt.Fprintf(os.Stderr, "Note: Morrow data is still in the old location next to the binary (%s); %s to move it.\n",
				config.GetDataDir(), privilege.MigrateHint)
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

// skipPreRun lists the commands that must work before 'morrow init' or must
// not print anything extra: init, help, the log relay, uninstall, and shell
// completion (the 'completion <shell>' scripts and cobra's hidden __complete
// requests sent on every Tab press).
func skipPreRun(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "init", "help", "_relay", "_runas-exec", "uninstall", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	}
	return cmd.HasParent() && cmd.Parent().Name() == "completion"
}

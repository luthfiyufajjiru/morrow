package cmd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"morrow/internal/config"
	"morrow/internal/db"
	"morrow/internal/privilege"
	"morrow/internal/uninstall"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	uninstallPurge  bool
	uninstallDryRun bool
	uninstallYes    bool
	uninstallBackup string
)

var UninstallCmd = &cobra.Command{
	Use:   "uninstall [--purge] [--dry-run] [--yes] [--backup-key <file>]",
	Short: "Stop all apps and remove what 'morrow init' set up (admin for a system install)",
	Long: `Stop every running app and remove the inbox (auto-apply hook, folder, keys and
deployers group). The database, master key and logs are kept, so reinstalling
and running 'morrow init' resumes with your apps and secrets.

--purge also deletes the database, master key and logs. Secured variables can
never be recovered without the key: use --backup-key to keep a copy.

The morrow binary itself is not removed.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if !config.IsPerUser() && !privilege.IsElevated() {
			log.Fatalf("Error: uninstalling the system install in %s requires elevation: %s",
				config.GetDataDir(), privilege.ElevationHint)
		}
		if uninstallBackup != "" && !uninstallPurge {
			log.Fatalf("Error: --backup-key only applies with --purge (without it the key is kept)")
		}
		if IsInitiated() {
			if err := db.InitDB(config.GetDBPath()); err != nil {
				log.Fatalf("failed to open db: %v", privilege.Hint(err))
			}
			if err := db.EnsureSchemaEmbedded(); err != nil {
				log.Fatalf("failed to migrate db: %v", privilege.Hint(err))
			}
		}

		plan, err := uninstall.Build(uninstallPurge)
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		for _, l := range plan.Lines() {
			fmt.Println(l)
		}
		if uninstallDryRun {
			fmt.Println("Dry run: nothing was changed.")
			return
		}
		if !uninstallYes && !confirm(os.Stdin, uninstallPurge) {
			fmt.Println("Aborted: nothing was changed.")
			os.Exit(1)
		}

		if err := uninstall.Execute(plan, uninstallBackup); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		uninstall.RemoveEmptyDirs(plan)

		if uninstallPurge {
			fmt.Println("Morrow data purged.")
			if uninstallBackup != "" {
				fmt.Printf("Master key copied to %s; keep it safe or delete it.\n", uninstallBackup)
			}
		} else {
			fmt.Println("Morrow stopped and the inbox removed. Database, key and logs were kept: reinstall and run 'morrow init' to resume.")
		}
		if exe, err := os.Executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				exe = real
			}
			fmt.Printf("The morrow binary was not removed: delete %s yourself (or uninstall the MSI/package that installed it).\n", exe)
		}
	},
}

// confirm asks before changing anything. Purging needs the word "purge"
// typed out; a plain uninstall needs "y". No input means no.
func confirm(in io.Reader, purge bool) bool {
	if purge {
		fmt.Print("This permanently deletes the data above. Type 'purge' to continue: ")
	} else {
		fmt.Print("Stop the apps and remove the items above? [y/N]: ")
	}
	line, _ := bufio.NewReader(in).ReadString('\n')
	answer := strings.TrimSpace(line)
	if purge {
		return answer == "purge"
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}

func init() {
	UninstallCmd.Flags().BoolVar(&uninstallPurge, "purge", false, "Also delete the database, master key and logs")
	UninstallCmd.Flags().BoolVar(&uninstallDryRun, "dry-run", false, "Show what would be stopped and removed, change nothing")
	UninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "Do not ask for confirmation")
	UninstallCmd.Flags().StringVar(&uninstallBackup, "backup-key", "", "With --purge, copy the master key to this new file first")
}

package cmd

import (
	"fmt"
	"log"
	"morrow/internal/app"
	"morrow/internal/config"
	"morrow/internal/privilege"
	"morrow/internal/startup"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	startupRemove  bool
	startupStatus  bool
	shutdownAlways bool
)

var StartupCmd = &cobra.Command{
	Use:   "startup [--status] [--remove]",
	Short: "Start apps at boot and stop them gracefully at shutdown (admin)",
	Long: `Install the OS hook that starts, at every boot, the apps you started and did
not stop yourself: a systemd unit on Linux (which also stops them gracefully
at shutdown), a scheduled task running as SYSTEM on Windows.

--status shows whether the hook is installed and which apps it would start.
--remove uninstalls the hook without stopping any app.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		wanted, err := app.WantedApps()
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		list := "none"
		if len(wanted) > 0 {
			list = strings.Join(wanted, ", ")
		}

		if startupStatus {
			if desc, ok := startup.Installed(); ok {
				fmt.Println("Installed:", desc)
			} else {
				fmt.Println("Not installed: apps are not started at boot. Run 'morrow startup' as an administrator.")
			}
			fmt.Println("Apps started at boot:", list)
			return
		}

		if config.IsPerUser() {
			log.Fatalf("Error: startup is for system installs; for a per-user install, run 'morrow _boot' with MORROW_HOME set from your own login or boot scripts")
		}
		if !privilege.IsElevated() {
			log.Fatalf("Error: installing or removing the startup hook requires elevation: %s", privilege.ElevationHint)
		}
		if startupRemove {
			if err := startup.Remove(); err != nil {
				log.Fatalf("Error: %v", err)
			}
			fmt.Println("Startup hook removed. Running apps were not stopped.")
			return
		}

		exe, err := os.Executable()
		if err == nil {
			exe, err = filepath.EvalSymlinks(exe)
		}
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		desc, err := startup.Install(exe)
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		fmt.Println("Installed", desc)
		fmt.Println("Apps started at boot:", list)
		fmt.Println("An app joins this list when you start it and leaves it when you run 'morrow stop'.")
	},
}

// BootCmd is run by the startup hook at boot.
var BootCmd = &cobra.Command{
	Use:    "_boot",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		lines, err := app.BootApps()
		if err != nil {
			log.Fatalf("boot: %v", err)
		}
		for _, l := range lines {
			fmt.Println("boot:", l)
		}
	},
}

// ShutdownCmd is run by the systemd unit when it stops. It only stops apps
// when the system is shutting down (or with --always), so removing or
// restarting the unit never takes apps down.
var ShutdownCmd = &cobra.Command{
	Use:    "_shutdown",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if !shutdownAlways && !startup.ShuttingDown() {
			fmt.Println("shutdown: the system is not shutting down; leaving apps running")
			return
		}
		lines, err := app.ShutdownApps(app.DefaultStopTimeout)
		if err != nil {
			log.Fatalf("shutdown: %v", err)
		}
		for _, l := range lines {
			fmt.Println("shutdown:", l)
		}
	},
}

func init() {
	StartupCmd.Flags().BoolVar(&startupRemove, "remove", false, "Uninstall the startup hook (running apps keep running)")
	StartupCmd.Flags().BoolVar(&startupStatus, "status", false, "Show whether the hook is installed and which apps start at boot")
	ShutdownCmd.Flags().BoolVar(&shutdownAlways, "always", false, "Stop the apps even if the system is not shutting down")
}

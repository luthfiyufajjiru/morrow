package app

import (
	"encoding/json"
	"fmt"
	"log"
	"morrow/internal/app"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var jsonOutput bool
var inlineEnvs []string
var forceDelete bool

var CreateAppCmd = &cobra.Command{
	Use:   "create [app-name] [executable-path] [args...]",
	Short: "Create a new managed application",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		execPath := args[1]
		var appArgs []string
		if len(args) > 2 {
			appArgs = args[2:]
		}

		if err := app.CreateApp(name, execPath, appArgs); err != nil {
			log.Fatalf("Error: %v", err)
		}
		fmt.Printf("Successfully created application %s\n", name)
	},
}

var DetailAppCmd = &cobra.Command{
	Use:   "detail [app-name]",
	Short: "Get detailed information for an application",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		detail, err := app.GetAppDetail(name)
		if err != nil {
			log.Fatalf("Error: %v", err)
		}

		if jsonOutput {
			data, _ := json.MarshalIndent(detail, "", "  ")
			fmt.Println(string(data))
			return
		}

		// Table output
		fmt.Println("+----------------------------+------------------------------------------+")
		fmt.Println("| Field                      | Value                                    |")
		fmt.Println("+----------------------------+------------------------------------------+")
		fmt.Printf("| %-26s | %-40s |\n", "Application ID", detail.ID)
		fmt.Printf("| %-26s | %-40s |\n", "Application Name", detail.Name)
		fmt.Printf("| %-26s | %-40s |\n", "Executable Path", detail.ExecutablePath)
		fmt.Printf("| %-26s | %-40s |\n", "Arguments", strings.Join(detail.Arguments, " "))
		fmt.Printf("| %-26s | %-40s |\n", "Full Command", detail.CommandLine)
		fmt.Printf("| %-26s | %-40s |\n", "Status", detail.Status)
		fmt.Printf("| %-26s | %-40d |\n", "PID", detail.PID)
		fmt.Printf("| %-26s | %-40s |\n", "Creation Time", detail.CreationTime.Format(time.RFC3339))
		fmt.Printf("| %-26s | %-40s |\n", "Update Time", detail.UpdateTime.Format(time.RFC3339))
		if detail.StatusTime != nil {
			fmt.Printf("| %-26s | %-40s |\n", "Status Time", detail.StatusTime.Format(time.RFC3339))
		}
		if detail.LastRunTime != nil {
			fmt.Printf("| %-26s | %-40s |\n", "Last Run Time", detail.LastRunTime.Format(time.RFC3339))
		}
		fmt.Println("+----------------------------+------------------------------------------+")
	},
}

var StartAppCmd = &cobra.Command{
	Use:   "start [app-name]",
	Short: "Start a managed application",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		envs := make(map[string]string)
		for _, e := range inlineEnvs {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envs[parts[0]] = parts[1]
			}
		}

		pid, err := app.StartApp(name, envs)
		if err != nil {
			log.Fatalf("Error starting app %s: %v", name, err)
		}
		fmt.Printf("Successfully started application %s with PID %d\n", name, pid)
	},
}

var StopAppCmd = &cobra.Command{
	Use:   "stop [app-name]",
	Short: "Stop a running application",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if err := app.StopApp(name); err != nil {
			log.Fatalf("Error stopping app %s: %v", name, err)
		}
		fmt.Printf("Successfully stopped application %s\n", name)
	},
}

var RestartAppCmd = &cobra.Command{
	Use:   "restart [app-name]",
	Short: "Restart an application",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		pid, err := app.RestartApp(name)
		if err != nil {
			log.Fatalf("Error restarting app %s: %v", name, err)
		}
		fmt.Printf("Successfully restarted application %s with PID %d\n", name, pid)
	},
}

var DeleteAppCmd = &cobra.Command{
	Use:   "delete [app-name]",
	Short: "Delete an application and its configuration",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if err := app.DeleteApp(name, forceDelete); err != nil {
			log.Fatalf("Error deleting app %s: %v", name, err)
		}
		fmt.Printf("Successfully deleted application %s\n", name)
	},
}

var ListAppsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all managed applications",
	Run: func(cmd *cobra.Command, args []string) {
		apps, err := app.ListApps()
		if err != nil {
			log.Fatalf("Error listing apps: %v", err)
		}
		if len(apps) == 0 {
			fmt.Println("No applications managed by Morrow.")
			return
		}
		fmt.Println("+----------------------------+---------+-------+")
		fmt.Println("| Name                       | Status  | PID   |")
		fmt.Println("+----------------------------+---------+-------+")
		for _, a := range apps {
			fmt.Printf("| %-26s | %-7s | %-5d |\n", a.Name, a.Status, a.PID)
		}
		fmt.Println("+----------------------------+---------+-------+")
	},
}

var StatusAppCmd = &cobra.Command{
	Use:   "status [app-name]",
	Short: "Get the simplified status of an application",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		status, pid, err := app.GetAppStatus(args[0])
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		fmt.Printf("Application: %s\nStatus: %s\nPID: %d\n", args[0], status, pid)
	},
}

var UpdateAppCmd = &cobra.Command{
	Use:   "update [app-name] [executable-path] [args...]",
	Short: "Update an application's configuration",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		execPath := args[1]
		var appArgs []string
		if len(args) > 2 {
			appArgs = args[2:]
		}
		if err := app.UpdateApp(name, execPath, appArgs); err != nil {
			log.Fatalf("Error: %v", err)
		}
		fmt.Printf("Successfully updated application %s\n", name)
	},
}

func init() {
	DetailAppCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	StartAppCmd.Flags().StringSliceVarP(&inlineEnvs, "env", "e", []string{}, "Inline environment variables (KEY=VALUE)")
	DeleteAppCmd.Flags().BoolVarP(&forceDelete, "force", "f", false, "Force delete even if running (stops the app first)")
}

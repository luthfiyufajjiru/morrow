package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"morrow/cmd/complete"
	"morrow/internal/app"
	"morrow/internal/config"
	"morrow/internal/privilege"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/nxadm/tail"
	"github.com/spf13/cobra"
)

var jsonOutput bool
var inlineEnvs []string
var forceDelete bool
var workDir string
var restartPolicy string
var maxRestarts int
var stopTimeout time.Duration
var runAs string
var passwordEnv string
var healthURL string
var healthInterval time.Duration
var healthFailures int

// applyRunAsFlags sets the run-as account when --user was given. The Windows
// password comes from the environment variable named by --password-env, so it
// never appears on the command line.
func applyRunAsFlags(cmd *cobra.Command, name string) error {
	if !cmd.Flags().Changed("user") {
		if passwordEnv != "" {
			return fmt.Errorf("--password-env only applies together with --user")
		}
		return nil
	}
	password := ""
	if passwordEnv != "" {
		v, ok := os.LookupEnv(passwordEnv)
		if !ok {
			return fmt.Errorf("environment variable %s (from --password-env) is not set", passwordEnv)
		}
		password = v
	}
	return app.SetRunAs(name, runAs, password)
}

// rootWarning is the warning shown when an app would run with the
// supervisor's own elevated account, or "" if it would not.
func rootWarning(name, runAs string, elevated bool, goos string) string {
	if runAs != "" || !elevated {
		return ""
	}
	who, hint := "root", "sudo morrow update "+name+" --user <account>"
	if goos == "windows" {
		who, hint = "an Administrator (as SYSTEM after a reboot)", "morrow update "+name+" --user <account> --password-env <VAR>"
	}
	return fmt.Sprintf("Warning: %s runs as %s, so if it is compromised it can read the master key and every app's secrets.\n"+
		"Run it as an unprivileged account instead: %s", name, who, hint)
}

// warnIfRoot prints rootWarning for the app to stderr.
func warnIfRoot(name string) {
	detail, err := app.GetAppDetail(name)
	if err != nil {
		return
	}
	if w := rootWarning(name, detail.RunAs, privilege.IsElevated(), runtime.GOOS); w != "" {
		fmt.Fprintln(os.Stderr, w)
	}
}

// applyHealthFlags sets the health check when any --health-* flag was given,
// keeping the current value of the others.
func applyHealthFlags(cmd *cobra.Command, name string) error {
	if !cmd.Flags().Changed("health-url") && !cmd.Flags().Changed("health-interval") && !cmd.Flags().Changed("health-failures") {
		return nil
	}
	detail, err := app.GetAppDetail(name)
	if err != nil {
		return err
	}
	u, interval, failures := detail.HealthURL, time.Duration(detail.HealthInterval)*time.Second, detail.HealthFailures
	if cmd.Flags().Changed("health-url") {
		u = healthURL
	}
	if cmd.Flags().Changed("health-interval") {
		interval = healthInterval
	}
	if cmd.Flags().Changed("health-failures") {
		failures = healthFailures
	}
	return app.SetHealthCheck(name, u, interval, failures)
}

// applyRestartFlags sets the restart policy when --restart or --max-restarts
// was given, keeping the current value of the other one.
func applyRestartFlags(cmd *cobra.Command, name string) error {
	if !cmd.Flags().Changed("restart") && !cmd.Flags().Changed("max-restarts") {
		return nil
	}
	detail, err := app.GetAppDetail(name)
	if err != nil {
		return err
	}
	policy, max := detail.RestartPolicy, detail.MaxRestarts
	if cmd.Flags().Changed("restart") {
		policy = restartPolicy
	}
	if cmd.Flags().Changed("max-restarts") {
		max = maxRestarts
	}
	return app.SetRestartPolicy(name, policy, max)
}

var CreateAppCmd = &cobra.Command{
	Use:   "create [flags] [app-name] [executable-path] [args...]",
	Short: "Create a new managed application",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		execPath := args[1]
		var appArgs []string
		if len(args) > 2 {
			appArgs = args[2:]
		}

		if cmd.Flags().Changed("restart") && !app.ValidPolicy(restartPolicy) {
			log.Fatalf("Error: unknown restart policy %q (use %s, %s or %s)", restartPolicy, app.PolicyNo, app.PolicyOnFailure, app.PolicyUnlessStopped)
		}
		if err := app.CreateApp(name, execPath, appArgs, workDir); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		if err := applyRestartFlags(cmd, name); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		if err := applyHealthFlags(cmd, name); err != nil {
			_ = app.DeleteApp(name, false) // do not leave a half-configured app behind
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		if err := applyRunAsFlags(cmd, name); err != nil {
			_ = app.DeleteApp(name, false) // do not leave a half-configured app behind
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		fmt.Printf("Successfully created application %s\n", name)
		warnIfRoot(name)
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
			log.Fatalf("Error: %v", privilege.Hint(err))
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
		fmt.Printf("| %-26s | %-40s |\n", "Working Directory", detail.WorkingDir)
		runAsText := detail.RunAs
		if runAsText == "" {
			runAsText = "(the supervisor's account)"
		}
		fmt.Printf("| %-26s | %-40s |\n", "Run As", runAsText)
		fmt.Printf("| %-26s | %-40s |\n", "Restart Policy",
			fmt.Sprintf("%s (max %d, restarted %d)", detail.RestartPolicy, detail.MaxRestarts, detail.RestartCount))
		health := "(none)"
		if detail.HealthURL != "" {
			health = fmt.Sprintf("GET %s every %ds, restart after %d failures", detail.HealthURL, detail.HealthInterval, detail.HealthFailures)
		}
		fmt.Printf("| %-26s | %-40s |\n", "Health Check", health)
		if detail.LastExitCode != nil {
			last := fmt.Sprintf("exit code %d", *detail.LastExitCode)
			if detail.LastExitSignal != "" {
				last = "killed by " + detail.LastExitSignal
			}
			if detail.LastExitTime != nil {
				last += " at " + detail.LastExitTime.Format(time.RFC3339)
			}
			fmt.Printf("| %-26s | %-40s |\n", "Last Exit", last)
		}
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
			log.Fatalf("Error starting app %s: %v", name, privilege.Hint(err))
		}
		fmt.Printf("Successfully started application %s with PID %d\n", name, pid)
		warnIfRoot(name)
	},
}

var StopAppCmd = &cobra.Command{
	Use:   "stop [app-name]",
	Short: "Stop a running application",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if err := app.StopAppTimeout(name, stopTimeout); err != nil {
			log.Fatalf("Error stopping app %s: %v", name, privilege.Hint(err))
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
		pid, err := app.RestartAppTimeout(name, stopTimeout)
		if err != nil {
			log.Fatalf("Error restarting app %s: %v", name, privilege.Hint(err))
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
			log.Fatalf("Error deleting app %s: %v", name, privilege.Hint(err))
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
			log.Fatalf("Error listing apps: %v", privilege.Hint(err))
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
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		fmt.Printf("Application: %s\nStatus: %s\nPID: %d\n", args[0], status, pid)
	},
}

var UpdateAppCmd = &cobra.Command{
	Use:   "update [flags] [app-name] [executable-path] [args...]",
	Short: "Update an application's configuration",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		execPath := args[1]
		var appArgs []string
		if len(args) > 2 {
			appArgs = args[2:]
		}
		var dir *string
		if cmd.Flags().Changed("workdir") {
			dir = &workDir
		}
		if err := app.UpdateApp(name, execPath, appArgs, dir); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		if err := applyRestartFlags(cmd, name); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		if err := applyHealthFlags(cmd, name); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		if err := applyRunAsFlags(cmd, name); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		fmt.Printf("Successfully updated application %s\n", name)
	},
}

var logsLines int
var followLogs bool

var LogsAppCmd = &cobra.Command{
	Use:   "logs [app-name]",
	Short: "Show logs for an application",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		appName := args[0]
		logPath := config.GetLogFilePath(appName)

		data, err := os.ReadFile(logPath)
		if errors.Is(err, os.ErrNotExist) {
			log.Fatalf("No logs found for %s (has it been started?)", appName)
		}
		if err != nil {
			log.Fatalf("Error reading logs: %v", privilege.Hint(err))
		}

		// Split in memory: unlike bufio.Scanner this has no per-line length limit.
		content := strings.TrimSuffix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		var lines []string
		if content != "" {
			lines = strings.Split(content, "\n")
		}

		start := 0
		if logsLines > 0 && len(lines) > logsLines {
			start = len(lines) - logsLines
		}

		// Print initial lines
		for _, l := range lines[start:] {
			fmt.Println(l)
		}

		// If follow mode is enabled, start tailing the file
		if followLogs {
			t, err := tail.TailFile(logPath, tail.Config{
				Follow:    true, // Keep reading as file grows
				ReOpen:    true, // Auto reopen if lumberjack rotates it
				MustExist: true,
				Poll:      true, // fsnotify often hangs on Windows when another process writes
				// Resume exactly where the initial read stopped so no lines are lost in between.
				Location: &tail.SeekInfo{Offset: int64(len(data)), Whence: io.SeekStart},
				Logger:   tail.DiscardingLogger,
			})
			if err != nil {
				log.Fatalf("Failed to follow logs: %v", privilege.Hint(err))
			}
			for line := range t.Lines {
				fmt.Println(line.Text)
			}
		}
	},
}

func init() {
	// Stop flag parsing at the first positional arg so app arguments such as
	// "--port 8080" are passed through instead of rejected as unknown flags.
	CreateAppCmd.Flags().SetInterspersed(false)
	UpdateAppCmd.Flags().SetInterspersed(false)
	CreateAppCmd.Flags().StringVarP(&workDir, "workdir", "w", "", "Working directory for the application")
	UpdateAppCmd.Flags().StringVarP(&workDir, "workdir", "w", "", "Working directory for the application (empty string clears it)")
	DetailAppCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	StartAppCmd.Flags().StringSliceVarP(&inlineEnvs, "env", "e", []string{}, "Inline environment variables (KEY=VALUE)")
	DeleteAppCmd.Flags().BoolVarP(&forceDelete, "force", "f", false, "Force delete even if running (stops the app first)")
	LogsAppCmd.Flags().IntVarP(&logsLines, "lines", "n", 50, "Number of recent lines to show (0 = all)")
	LogsAppCmd.Flags().BoolVarP(&followLogs, "follow", "f", false, "Follow log output continuously")
	for _, c := range []*cobra.Command{CreateAppCmd, UpdateAppCmd} {
		c.Flags().StringVar(&restartPolicy, "restart", app.PolicyNo,
			"What to do when the app exits without 'morrow stop': no, on-failure (restart after a non-zero exit or a signal) or unless-stopped (restart after any exit)")
		c.Flags().IntVar(&maxRestarts, "max-restarts", 5,
			"With a restart policy: give up after this many restarts in a row (an app that ran 60s+ resets the count)")
		_ = c.RegisterFlagCompletionFunc("restart", cobra.FixedCompletions([]string{app.PolicyNo, app.PolicyOnFailure, app.PolicyUnlessStopped}, cobra.ShellCompDirectiveNoFileComp))
	}
	for _, c := range []*cobra.Command{CreateAppCmd, UpdateAppCmd} {
		c.Flags().StringVar(&runAs, "user", "",
			"Run the app as this account instead of root/Administrator (empty clears it); Windows also needs --password-env")
		c.Flags().StringVar(&passwordEnv, "password-env", "",
			"Windows: name of an environment variable holding the --user account's password (stored encrypted)")
	}
	for _, c := range []*cobra.Command{CreateAppCmd, UpdateAppCmd} {
		c.Flags().StringVar(&healthURL, "health-url", "",
			"HTTP(S) URL the relay checks while the app runs; a status of 400+ or no answer is a failure (empty removes the check)")
		c.Flags().DurationVar(&healthInterval, "health-interval", app.DefaultHealthInterval,
			"How often to check --health-url (whole seconds; the first check is one interval after the start)")
		c.Flags().IntVar(&healthFailures, "health-failures", app.DefaultHealthFailures,
			"Restart the app after this many failed health checks in a row (counts against --max-restarts)")
	}
	for _, c := range []*cobra.Command{StopAppCmd, RestartAppCmd} {
		c.Flags().DurationVar(&stopTimeout, "timeout", app.DefaultStopTimeout,
			"How long to wait after asking the app to exit (SIGTERM) before killing it")
	}
}

func init() {
	CreateAppCmd.ValidArgsFunction = complete.NewAppThenFiles
	UpdateAppCmd.ValidArgsFunction = complete.AppThenFiles
	for _, c := range []*cobra.Command{DetailAppCmd, StartAppCmd, StopAppCmd, RestartAppCmd, DeleteAppCmd, StatusAppCmd, LogsAppCmd} {
		c.ValidArgsFunction = complete.App
	}
	ListAppsCmd.ValidArgsFunction = complete.None
	_ = CreateAppCmd.MarkFlagDirname("workdir")
	_ = UpdateAppCmd.MarkFlagDirname("workdir")
}

package env

import (
	"fmt"
	"log"
	"morrow/internal/env"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var secured bool

var SetEnvCmd = &cobra.Command{
	Use:   "set-env [app-name] [key=value...]",
	Short: "Set one or more environment variables for an application",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		appName := args[0]

		for i := 1; i < len(args); i++ {
			kv := strings.SplitN(args[i], "=", 2)
			if len(kv) != 2 {
				log.Fatalf("Error: Invalid env variable format at '%s', use key=value", args[i])
			}

			key := kv[0]
			val := kv[1]
			isSecuredArg := secured // Default to global --secured flag

			// Check for per-variable overrides (key:s=value or key:u=value)
			if strings.HasSuffix(key, ":s") {
				key = strings.TrimSuffix(key, ":s")
				isSecuredArg = true
			} else if strings.HasSuffix(key, ":u") {
				key = strings.TrimSuffix(key, ":u")
				isSecuredArg = false
			}

			if err := env.SetEnv(appName, key, val, isSecuredArg); err != nil {
				log.Fatalf("Error: %v", err)
			}

			currentStatus := "unsecured"
			if isSecuredArg {
				currentStatus = "secured (encrypted)"
			}
			fmt.Printf("Successfully set %s as %s for application %s\n", key, currentStatus, appName)
		}
	},
}

var GetEnvCmd = &cobra.Command{
	Use:   "get-env [app-name] [key]",
	Short: "Get a specific environment variable for an application",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		val, isSecured, err := env.GetEnv(args[0], args[1])
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		status := ""
		if isSecured {
			status = " [SECURED]"
		}
		fmt.Printf("%s=%s%s\n", args[1], val, status)
	},
}

var DelEnvCmd = &cobra.Command{
	Use:   "del-env [app-name] [key]",
	Short: "Delete an environment variable for an application",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		if err := env.DelEnv(args[0], args[1]); err != nil {
			log.Fatalf("Error: %v", err)
		}
		fmt.Printf("Successfully deleted %s for application %s\n", args[1], args[0])
	},
}

var ListEnvCmd = &cobra.Command{
	Use:   "list-env [app-name]",
	Short: "List all environment variables for an application",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		envs, err := env.ListEnv(args[0])
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		fmt.Printf("Environment variables for %s:\n", args[0])
		isRoot := os.Geteuid() == 0
		for _, e := range envs {
			val := e.Value
			status := ""
			if e.IsSecured {
				if !isRoot {
					val = "****"
				}
				status = " [SECURED]"
			}
			fmt.Printf("%s=%s%s\n", e.Name, val, status)
		}
	},
}

func init() {
	SetEnvCmd.Flags().BoolVarP(&secured, "secured", "s", false, "Encrypt the environment variable")
}

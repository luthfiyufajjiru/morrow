// Package inbox holds the commands for write-only secret submission:
// administrators grant variables, deployers submit them, and apply stores them.
package inbox

import (
	"fmt"
	"log"
	"morrow/cmd/complete"
	"morrow/internal/grant"
	"morrow/internal/inbox"
	"morrow/internal/privilege"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	grantApp    string
	grantVars   []string
	grantFormat string
	restartFlag bool
	eventsLimit int
)

var GrantCmd = &cobra.Command{
	Use:   "grant <user> --app <app> --vars <NAME,...> [--format token]",
	Short: "Allow a non-admin user to submit write-only secrets for an app (admin)",
	Long: `Allow <user> to submit the listed variables of an app through the inbox
with 'morrow submit-env'. The user can set these values but never read them.

Formats: token (default: 8-512 chars of A-Z a-z 0-9 . _ ~ + / = -), int, bool,
url (https, no embedded credentials), enum:a|b|c, regex:<pattern>, any.
Names that make programs load other code (PATH, LD_*, NODE_OPTIONS, PYTHON*, ...)
are refused.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		who, err := grant.ResolvePrincipal(args[0])
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		if err := grant.Add(who, grantApp, grantVars, grantFormat); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		fmt.Printf("Granted %s: %s of %s (format %s)\n", who, strings.Join(grantVars, ", "), grantApp, grantFormat)
		if grantFormat == "any" {
			fmt.Println("Warning: format 'any' accepts every value. Prefer a stricter format: the app runs with administrator rights.")
		}
		fmt.Printf("Make sure %s is a member of the %q group so it can write to the inbox.\n", who, privilege.DeployGroup)
	},
}

var RevokeCmd = &cobra.Command{
	Use:   "revoke <user> --app <app> [--vars <NAME,...>]",
	Short: "Remove a user's grants for an app; all of them unless --vars is given (admin)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		who, err := grant.ResolvePrincipal(args[0])
		if err != nil {
			who = grant.NormalizePrincipal(args[0]) // allow revoking for deleted accounts
		}
		n, err := grant.Remove(who, grantApp, grantVars)
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		fmt.Printf("Removed %d grant(s) of %s for %s\n", n, who, grantApp)
	},
}

var GrantsCmd = &cobra.Command{
	Use:   "grants",
	Short: "List who may submit which variables",
	Run: func(cmd *cobra.Command, args []string) {
		gs, err := grant.List()
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		if len(gs) == 0 {
			fmt.Println("No grants.")
			return
		}
		fmt.Printf("%-24s %-20s %-24s %s\n", "USER", "APP", "VARIABLE", "FORMAT")
		for _, g := range gs {
			fmt.Printf("%-24s %-20s %-24s %s\n", g.Principal, g.App, g.EnvName, g.Format)
		}
	},
}

var SubmitEnvCmd = &cobra.Command{
	Use:   "submit-env <app> <KEY[=VALUE]>... [--restart]",
	Short: "Submit write-only secrets for an app (no admin rights needed, needs a grant)",
	Long: `Seal each value with the install's public key and drop it into the inbox.
You cannot read the value back; an administrator's 'morrow apply' (or the hook
installed by 'morrow init --enable-inbox') stores it.

KEY=VALUE submits VALUE. A bare KEY reads the value from your own environment
variable KEY, which keeps it out of the process list and shell history:

  API_TOKEN="$CI_SECRET" morrow submit-env api API_TOKEN --restart`,
	Args: cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		pairs := make([]inbox.Pair, 0, len(args)-1)
		for _, a := range args[1:] {
			name, value, ok := strings.Cut(a, "=")
			if !ok {
				v, found := os.LookupEnv(name)
				if !found {
					log.Fatalf("Error: %s has no value and is not set in your environment", name)
				}
				value = v
			}
			pairs = append(pairs, inbox.Pair{Name: name, Value: value})
		}
		ids, err := inbox.Submit(args[0], pairs, restartFlag)
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		fmt.Printf("Submitted %d variable(s) for %s. They take effect when 'morrow apply' runs; "+
			"check the outcome with 'morrow inbox'.\n", len(ids), args[0])
		for i, id := range ids {
			fmt.Printf("  %s  %s\n", id, pairs[i].Name)
		}
	},
}

var ApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Store submitted secrets from the inbox (admin; normally run by the OS hook)",
	Run: func(cmd *cobra.Command, args []string) {
		res, err := inbox.Apply()
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		fmt.Printf("Applied %d, rejected %d submission(s).\n", res.Applied, res.Rejected)
		if len(res.Restarted) > 0 {
			fmt.Printf("Restarted: %s\n", strings.Join(res.Restarted, ", "))
		}
		for _, e := range res.Errors {
			fmt.Println("Error:", e)
		}
		if res.Rejected > 0 {
			fmt.Println("See 'morrow inbox' for the reasons.")
		}
		if len(res.Errors) > 0 {
			os.Exit(1)
		}
	},
}

var InboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Show the outcome of recent submissions (never shows values)",
	Run: func(cmd *cobra.Command, args []string) {
		events, err := inbox.Events(eventsLimit)
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		if len(events) == 0 {
			fmt.Println("No submissions processed yet.")
			return
		}
		for _, e := range events {
			line := fmt.Sprintf("%s  %-8s %-16s %-20s %-20s", e.Time, e.Status, e.Principal, e.App, e.EnvName)
			if e.Reason != "" {
				line += "  " + e.Reason
			}
			fmt.Println(strings.TrimRight(line, " "))
		}
	},
}

func init() {
	GrantCmd.Flags().StringVar(&grantApp, "app", "", "Application name")
	GrantCmd.Flags().StringSliceVar(&grantVars, "vars", nil, "Variable names, comma separated")
	GrantCmd.Flags().StringVar(&grantFormat, "format", "token", "Required value format")
	_ = GrantCmd.MarkFlagRequired("app")
	_ = GrantCmd.MarkFlagRequired("vars")

	RevokeCmd.Flags().StringVar(&grantApp, "app", "", "Application name")
	RevokeCmd.Flags().StringSliceVar(&grantVars, "vars", nil, "Variable names (default: all)")
	_ = RevokeCmd.MarkFlagRequired("app")

	SubmitEnvCmd.Flags().BoolVar(&restartFlag, "restart", false, "Restart the app after the values are applied (if it is running)")
	InboxCmd.Flags().IntVarP(&eventsLimit, "lines", "n", 20, "Number of events to show")
}

func init() {
	SubmitEnvCmd.ValidArgsFunction = complete.AppThenAssignments
	for _, c := range []*cobra.Command{GrantCmd, RevokeCmd} {
		_ = c.RegisterFlagCompletionFunc("app", complete.AppFlag)
	}
	for _, c := range []*cobra.Command{GrantsCmd, ApplyCmd, InboxCmd} {
		c.ValidArgsFunction = complete.None
	}
}

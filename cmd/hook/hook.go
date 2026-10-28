// Package hook holds the commands that manage alert hooks.
package hook

import (
	"fmt"
	"log"
	"morrow/cmd/complete"
	"morrow/internal/hooks"
	"morrow/internal/privilege"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	events  []string
	execCmd string
	webhook string
)

var HookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Alerts when an app exits unexpectedly: run a command or call a webhook",
	Long: `Hooks run when an app exits without 'morrow stop':

  crash   it exited with a non-zero code or was killed by a signal
  exit    it exited with code 0
  giveup  its restart policy stopped restarting it

A command hook runs with MORROW_EVENT, MORROW_APP_NAME, MORROW_APP_ID,
MORROW_EXIT_CODE, MORROW_EXIT_SIGNAL, MORROW_RESTART_COUNT, MORROW_EVENT_TIME
and MORROW_HOST set. A webhook receives the same fields as a JSON POST.
Hooks run as the supervisor (root/Administrator on a system install) and are
stored encrypted, so only administrators can add or read them.`,
}

var addCmd = &cobra.Command{
	Use:   "add <app> --on crash,giveup (--exec <command> | --webhook <url>)",
	Short: "Add an alert hook to an app (admin)",
	Example: `  sudo morrow hook add api --on crash,giveup --webhook https://hooks.slack.com/services/T000/B000/XXXX
  sudo morrow hook add api --on giveup --exec 'logger -t morrow "$MORROW_APP_NAME gave up after $MORROW_RESTART_COUNT restarts"'`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: complete.App,
	Run: func(cmd *cobra.Command, args []string) {
		kind, target := hooks.KindExec, execCmd
		switch {
		case execCmd != "" && webhook != "":
			log.Fatalf("Error: use either --exec or --webhook, not both")
		case webhook != "":
			kind, target = hooks.KindWebhook, webhook
		case execCmd == "":
			log.Fatalf("Error: give a command with --exec or a URL with --webhook")
		}
		id, err := hooks.Add(args[0], events, kind, target)
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		fmt.Printf("Added hook %s to %s (%s on %s). Try it with 'morrow hook test %s %s'.\n",
			id, args[0], kind, strings.Join(events, ","), args[0], id)
	},
}

var listCmd = &cobra.Command{
	Use:               "list [app]",
	Short:             "List alert hooks (targets are shown only to administrators)",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: complete.App,
	Run: func(cmd *cobra.Command, args []string) {
		app := ""
		if len(args) == 1 {
			app = args[0]
		}
		hs, err := hooks.List(app)
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		if len(hs) == 0 {
			fmt.Println("No hooks.")
			return
		}
		fmt.Printf("%-10s %-20s %-20s %-8s %s\n", "ID", "APP", "EVENTS", "KIND", "TARGET")
		for _, h := range hs {
			fmt.Printf("%-10s %-20s %-20s %-8s %s\n", h.ID, h.App, strings.Join(h.Events, ","), h.Kind, h.Target)
		}
	},
}

var removeCmd = &cobra.Command{
	Use:               "remove <app> <id>",
	Short:             "Remove an alert hook (admin)",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeHookIDs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := hooks.Remove(args[0], args[1]); err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		fmt.Printf("Removed hook %s from %s\n", args[1], args[0])
	},
}

var testCmd = &cobra.Command{
	Use:               "test <app> <id>",
	Short:             "Run a hook now with a sample crash event (admin)",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeHookIDs,
	Run: func(cmd *cobra.Command, args []string) {
		hs, err := hooks.List(args[0])
		if err != nil {
			log.Fatalf("Error: %v", privilege.Hint(err))
		}
		for _, h := range hs {
			if h.ID != args[1] {
				continue
			}
			if h.Target == "****" {
				log.Fatalf("Error: cannot decrypt this hook: %s", privilege.ElevationHint)
			}
			ev := hooks.Event{Name: hooks.EventCrash, App: args[0], ExitCode: 1, Time: time.Now().UTC()}
			if err := hooks.Run(h, ev); err != nil {
				log.Fatalf("Hook %s failed: %v", h.ID, err)
			}
			target := h.Target
			if h.Kind == hooks.KindWebhook {
				if u, err := url.Parse(h.Target); err == nil {
					target = u.Host
				}
			}
			fmt.Printf("Hook %s ran (%s to %s)\n", h.ID, h.Kind, target)
			return
		}
		log.Fatalf("Error: hook %s not found for %s", args[1], args[0])
	},
}

// completeHookIDs completes an app, then that app's hook IDs.
func completeHookIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return complete.App(cmd, args, toComplete)
	}
	if len(args) > 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	hs, _ := hooks.List(args[0])
	var ids []string
	for _, h := range hs {
		if strings.HasPrefix(h.ID, toComplete) {
			ids = append(ids, h.ID+"\t"+h.Kind+" on "+strings.Join(h.Events, ","))
		}
	}
	return ids, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	addCmd.Flags().StringSliceVar(&events, "on", []string{hooks.EventCrash, hooks.EventGiveUp, hooks.EventUnhealthy}, "Events: crash, exit, giveup, unhealthy")
	addCmd.Flags().StringVar(&execCmd, "exec", "", "Command to run (sh -c on Linux/macOS, cmd /C on Windows)")
	addCmd.Flags().StringVar(&webhook, "webhook", "", "URL to POST a JSON event to")
	_ = addCmd.RegisterFlagCompletionFunc("on", cobra.FixedCompletions([]string{hooks.EventCrash, hooks.EventExit, hooks.EventGiveUp, hooks.EventUnhealthy}, cobra.ShellCompDirectiveNoFileComp))
	HookCmd.AddCommand(addCmd, listCmd, removeCmd, testCmd)
}

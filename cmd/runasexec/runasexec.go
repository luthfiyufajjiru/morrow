// Package runasexec is the hidden helper Morrow starts as another Windows
// account: it connects to the supervisor's named pipe, runs the app with the
// pipe as its output, and exits with the app's exit code.
package runasexec

import (
	"github.com/spf13/cobra"
)

// RunAsExecCmd is `morrow _runas-exec <pipe> -- <executable> [args...]`.
var RunAsExecCmd = &cobra.Command{
	Use:                "_runas-exec <pipe> -- <executable> [args...]",
	Hidden:             true,
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		run(args)
	},
}

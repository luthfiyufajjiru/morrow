//go:build !windows

package runasexec

import "os"

// run is Windows-only: Unix starts the app as the account directly.
func run([]string) { os.Exit(125) }

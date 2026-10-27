//go:build windows

package runasexec

import (
	"os"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func run(args []string) {
	if len(args) < 3 || args[1] != "--" {
		os.Exit(125)
	}
	out, err := os.OpenFile(args[0], os.O_WRONLY, 0)
	if err != nil {
		os.Exit(126)
	}
	app := exec.Command(args[2], args[3:]...)
	app.Stdout, app.Stderr = out, out
	app.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if err := app.Start(); err != nil {
		_, _ = out.WriteString("morrow: cannot start the app: " + err.Error() + "\n")
		os.Exit(127)
	}
	_ = app.Wait()
	out.Close()
	os.Exit(app.ProcessState.ExitCode())
}

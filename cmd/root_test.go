package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestSkipPreRun(t *testing.T) {
	completion := &cobra.Command{Use: "completion"}
	bash := &cobra.Command{Use: "bash"}
	completion.AddCommand(bash)
	cases := []struct {
		cmd  *cobra.Command
		want bool
	}{
		{&cobra.Command{Use: "init"}, true},
		{&cobra.Command{Use: "uninstall"}, true},
		{&cobra.Command{Use: "_relay"}, true},
		{&cobra.Command{Use: "_runas-exec"}, true},
		{&cobra.Command{Use: cobra.ShellCompRequestCmd}, true},
		{&cobra.Command{Use: cobra.ShellCompNoDescRequestCmd}, true},
		{bash, true},
		{&cobra.Command{Use: "bash"}, false}, // only under 'completion'
		{&cobra.Command{Use: "list"}, false},
		{&cobra.Command{Use: "set-env"}, false},
	}
	for _, c := range cases {
		if got := skipPreRun(c.cmd); got != c.want {
			t.Errorf("skipPreRun(%s) = %v, want %v", c.cmd.CommandPath(), got, c.want)
		}
	}
}

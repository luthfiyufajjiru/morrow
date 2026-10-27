package app

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func resetFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	t.Cleanup(func() {
		workDir = ""
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	})
}

func TestWorkdirFlagParsing(t *testing.T) {
	cmds := map[string]*cobra.Command{"create": CreateAppCmd, "update": UpdateAppCmd}
	for name, cmd := range cmds {
		t.Run(name, func(t *testing.T) {
			resetFlags(t, cmd)
			dir := t.TempDir()
			in := []string{"-w", dir, "myapp", "python", "main.py", "--port", "8080"}
			if err := cmd.Flags().Parse(in); err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if workDir != dir {
				t.Errorf("workDir = %q, want %q", workDir, dir)
			}
			if !cmd.Flags().Changed("workdir") {
				t.Error("workdir not marked changed")
			}
			want := []string{"myapp", "python", "main.py", "--port", "8080"}
			if got := cmd.Flags().Args(); !reflect.DeepEqual(got, want) {
				t.Errorf("Args = %v, want %v", got, want)
			}
		})
	}
}

func TestPassThroughWithoutWorkdir(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{"create": CreateAppCmd, "update": UpdateAppCmd} {
		t.Run(name, func(t *testing.T) {
			resetFlags(t, cmd)
			if err := cmd.Flags().Parse([]string{"myapp", "node", "--inspect", "-w", "x"}); err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cmd.Flags().Changed("workdir") || workDir != "" {
				t.Errorf("flag after positional must not be parsed; workDir=%q", workDir)
			}
			want := []string{"myapp", "node", "--inspect", "-w", "x"}
			if got := cmd.Flags().Args(); !reflect.DeepEqual(got, want) {
				t.Errorf("Args = %v, want %v", got, want)
			}
		})
	}
}

func TestEmptyWorkdirIsChanged(t *testing.T) {
	resetFlags(t, UpdateAppCmd)
	if err := UpdateAppCmd.Flags().Parse([]string{"--workdir=", "myapp", "exe"}); err != nil {
		t.Fatal(err)
	}
	if !UpdateAppCmd.Flags().Changed("workdir") || workDir != "" {
		t.Errorf("empty --workdir should be Changed with empty value; changed=%v val=%q",
			UpdateAppCmd.Flags().Changed("workdir"), workDir)
	}
}

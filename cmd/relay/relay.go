package relay

import (
	"encoding/json"
	"fmt"
	"morrow/internal/app"
	"morrow/internal/config"
	"morrow/internal/db"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

// RelayCmd is the hidden supervisor launched by "morrow start". It starts the
// app as its own child, writes the app's output to a rotating log file, and
// when the app exits records the exit code, applies the restart policy and
// runs alert hooks. It reports the first start on stdout to `morrow start`.
var RelayCmd = &cobra.Command{
	Use:    "_relay [app-name]",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		appName := args[0]
		var req app.StartRequest
		if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
			fmt.Println("error invalid start request:", err)
			return
		}
		os.Stdin.Close()

		logger := &lumberjack.Logger{
			Filename:   config.GetLogFilePath(appName),
			MaxSize:    10,   // MB before rotation
			MaxBackups: 3,    // keep last 3 rotated files
			MaxAge:     28,   // days
			Compress:   true, // gzip rotated files
		}
		defer logger.Close()
		defer removeOwnPIDFile(config.GetRelayPIDFilePath(appName))

		if err := db.InitDB(config.GetDBPath()); err != nil {
			fmt.Println("error cannot open database:", err)
			return
		}
		defer db.DB.Close()

		_ = app.Supervise(appName, req, os.Stdout, logger)
	},
}

// removeOwnPIDFile deletes the relay PID file only if it still holds this
// process's PID (a newer relay may have replaced it).
func removeOwnPIDFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid == os.Getpid() {
		_ = os.Remove(path)
	}
}

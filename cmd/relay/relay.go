package relay

import (
	"fmt"
	"io"
	"log"
	"morrow/internal/app"
	"morrow/internal/config"
	"morrow/internal/db"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

// RelayCmd is an internal hidden command launched as a sidecar by "morrow start".
// It pipes the managed app's stdout/stderr into a rotating log file. When the
// pipe closes (the app exited), it records a requested stop, or applies the
// app's restart policy to an unexpected exit.
var RelayCmd = &cobra.Command{
	Use:    "_relay [app-name] [app-pid]",
	Hidden: true,
	Args:   cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		appName := args[0]
		started := time.Now()

		logger := &lumberjack.Logger{
			Filename:   config.GetLogFilePath(appName),
			MaxSize:    10,   // MB before rotation
			MaxBackups: 3,    // keep last 3 rotated files
			MaxAge:     28,   // days
			Compress:   true, // gzip rotated files
		}

		// Block until the pipe write-end closes (app has exited or been killed).
		_, _ = io.Copy(logger, os.Stdin)
		logf := func(line string) {
			_, _ = fmt.Fprintf(logger, "%s %s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"), line)
		}
		defer logger.Close()
		// After an automatic restart the file names the new relay; leave it alone.
		defer removeOwnPIDFile(config.GetRelayPIDFilePath(appName))

		if err := db.InitDB(config.GetDBPath()); err != nil {
			log.Printf("relay: cannot open database for %s: %v", appName, err)
			return
		}
		defer db.DB.Close()

		pid := 0
		if len(args) == 2 {
			pid, _ = strconv.Atoi(args[1])
		}
		if pid <= 0 {
			// Relays started by an older morrow do not know the PID: only record the stop.
			_, _ = db.DB.Exec(`UPDATE applications SET application_status = 'stopped', application_pid = 0
				WHERE application_name = ? AND application_status IN ('running', 'stopping')`, appName)
			return
		}
		if err := app.HandleExit(appName, pid, time.Since(started), logf); err != nil {
			logf(fmt.Sprintf("[morrow] failed to record the exit: %v", err))
		}
	},
}

// removeOwnPIDFile deletes the relay PID file only if it still holds this
// process's PID.
func removeOwnPIDFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid == os.Getpid() {
		_ = os.Remove(path)
	}
}

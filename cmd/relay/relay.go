package relay

import (
	"io"
	"log"
	"morrow/internal/config"
	"morrow/internal/db"
	"os"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

// RelayCmd is an internal hidden command launched as a sidecar by "morrow start".
// It pipes the managed app's stdout/stderr into a rotating log file.
// When the pipe closes (app exited), it updates the DB status to "stopped".
var RelayCmd = &cobra.Command{
	Use:    "_relay [app-name]",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		appName := args[0]

		logger := &lumberjack.Logger{
			Filename:   config.GetLogFilePath(appName),
			MaxSize:    10,   // MB before rotation
			MaxBackups: 3,    // keep last 3 rotated files
			MaxAge:     28,   // days
			Compress:   true, // gzip rotated files
		}

		// Block until the pipe write-end closes (app has exited or been killed).
		_, _ = io.Copy(logger, os.Stdin)
		_ = logger.Close()

		// App is gone — open DB briefly to mark it as stopped.
		if err := db.InitDB(config.GetDBPath()); err == nil {
			now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
			_, dbErr := db.DB.Exec(`
				UPDATE applications
				SET application_status = 'stopped', application_pid = 0,
				    application_status_time = ?, application_update_time = ?
				WHERE application_name = ?
			`, now, now, appName)
			if dbErr != nil {
				log.Printf("relay: failed to update status for %s: %v", appName, dbErr)
			}
			db.DB.Close()
		}

		// Clean up own PID file.
		_ = os.Remove(config.GetRelayPIDFilePath(appName))
	},
}

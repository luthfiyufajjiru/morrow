package main

import (
	"morrow/cmd"
	appCmd "morrow/cmd/app"
	"morrow/cmd/env"
	"morrow/cmd/relay"
)

func main() {
	cmd.RegisterCommands(
		cmd.InitCmd,
		appCmd.CreateAppCmd,
		appCmd.DetailAppCmd,
		appCmd.StartAppCmd,
		appCmd.StopAppCmd,
		appCmd.RestartAppCmd,
		appCmd.DeleteAppCmd,
		appCmd.ListAppsCmd,
		appCmd.StatusAppCmd,
		appCmd.UpdateAppCmd,
		appCmd.LogsAppCmd,
		env.SetEnvCmd,
		env.GetEnvCmd,
		env.DelEnvCmd,
		env.ListEnvCmd,
		relay.RelayCmd,
	)
	cmd.Execute()
}

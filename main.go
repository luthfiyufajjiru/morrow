package main

import (
	"morrow/cmd"
	appCmd "morrow/cmd/app"
	"morrow/cmd/env"
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
		env.SetEnvCmd,
		env.GetEnvCmd,
		env.DelEnvCmd,
		env.ListEnvCmd,
	)
	cmd.Execute()
}

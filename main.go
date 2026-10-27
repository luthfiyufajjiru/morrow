package main

import (
	"morrow/cmd"
	appCmd "morrow/cmd/app"
	"morrow/cmd/env"
	"morrow/cmd/hook"
	"morrow/cmd/inbox"
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
		inbox.GrantCmd,
		inbox.RevokeCmd,
		inbox.GrantsCmd,
		inbox.SubmitEnvCmd,
		inbox.ApplyCmd,
		inbox.InboxCmd,
		hook.HookCmd,
		cmd.UninstallCmd,
		cmd.StartupCmd,
		cmd.BootCmd,
		cmd.ShutdownCmd,
		relay.RelayCmd,
	)
	cmd.Execute()
}

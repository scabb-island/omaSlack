// Command omaslack is the single binary behind the omaslack Slack client:
// daemon, terminal client, control CLI and setup tooling.
package main

import (
	"context"
	"os"

	"gitlab.com/scabb-island/omaslack/internal/cli"
	"gitlab.com/scabb-island/omaslack/internal/console"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
)

func main() {
	os.Exit(cli.Run(cli.Env{
		Stdout:            os.Stdout,
		Stderr:            os.Stderr,
		Stdin:             os.Stdin,
		StdinIsTerminal:   console.IsTerminal(os.Stdin),
		Keyring:           keyring.Open,
		DesktopWorkspaces: desktopWorkspaces,
		Browsers:          browsers,
		SlackAPIURL:       desktopsession.DefaultAPIURL,
	}, os.Args[1:]))
}

// desktopWorkspaces reads the installed Slack desktop app's workspaces.
func desktopWorkspaces(ctx context.Context) ([]auth.Workspace, error) {
	dir, err := auth.FindDesktopDir()
	if err != nil {
		return nil, err
	}
	return auth.Desktop{Dir: dir, Passwords: auth.SystemPasswords{}}.Workspaces(ctx)
}

// browsers lists the Chromium browsers whose Slack web session can be read.
func browsers() []cli.BrowserSession {
	found := auth.FindBrowsers()
	sources := make([]cli.BrowserSession, len(found))
	for i, b := range found {
		sources[i] = cli.BrowserSession{Name: b.Name, Workspaces: b.Workspaces}
	}
	return sources
}

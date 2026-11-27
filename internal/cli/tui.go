package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"gitlab.com/scabb-island/omaslack/internal/tui/app"
	"gitlab.com/scabb-island/omaslack/internal/tui/image"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
)

// tuiUsage is the usage of `omaslack tui`.
const tuiUsage = `Usage: omaslack tui

Opens the terminal client on the daemon, which is started with
systemctl --user start omaslack if it is not running.

Colors come from the Omarchy theme (omaslack.toml, else colors.toml) and
reload on SIGUSR1. Press ? inside for the keys.

To open it the Omarchy way, with the window class org.omarchy.omaslack
so Hyprland rules and shortcuts can target it, and to focus the open
window instead of starting another:

  omarchy-launch-or-focus-tui omaslack tui

Exit codes:
  0  quit normally
  1  any other error
  2  usage error
  5  the daemon is not running and could not be started
`

// tuiClient is the name the TUI gives the daemon in hello.
const tuiClient = "omaslack-tui"

// runTUI runs the terminal client until the user quits.
func runTUI(env Env, args []string) error {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_, err := fmt.Fprint(env.Stdout, tuiUsage)
		return err
	}
	if len(args) > 0 {
		return &usageError{err: fmt.Errorf("unexpected arguments: %v", args), usage: tuiUsage}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := dialDaemon(ctx, env, tuiClient)
	if err != nil {
		return err
	}
	defer c.Close()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1)
	defer signal.Stop(sigs)
	run := env.RunTUI
	if run == nil {
		run = app.Run
	}
	return run(ctx, app.Options{
		Client: c,
		Theme:  initialTheme(env),
		Themes: theme.Watch(ctx, loadTheme, sigs),
		Images: image.Detect(os.Getenv),
	})
}

// loadTheme reads the active Omarchy theme.
func loadTheme() (theme.Theme, error) {
	p, err := theme.DefaultPaths()
	if err != nil {
		return theme.Theme{}, err
	}
	return theme.Load(p)
}

// initialTheme is the Omarchy theme, or the default one when there is
// none; any other problem is reported before the screen takes over.
func initialTheme(env Env) theme.Theme {
	t, err := loadTheme()
	if err == nil {
		return t
	}
	if !errors.Is(err, theme.ErrNoTheme) {
		fmt.Fprintf(env.Stderr, "omaslack tui: using the default theme: %v\n", err)
	}
	return theme.Default()
}

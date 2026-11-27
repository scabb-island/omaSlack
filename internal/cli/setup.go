package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/doctor"
	"gitlab.com/scabb-island/omaslack/internal/setup"
)

// setupUsage is the usage of `omaslack setup`.
const setupUsage = `Usage: omaslack setup [--window-rule] [--remove] [--dry-run] [--from <dir>]

Sets omaslack up for this user, after the package is installed (the
package itself never writes to your home directory):
  - copies the theme template to ~/.config/omarchy/themed/omaslack.toml.tpl
  - copies the theme hook to ~/.config/omarchy/hooks/theme-set.d/omaslack
  - renders the current theme with omarchy-theme-refresh, if needed
  - enables and starts the user service (systemctl --user enable --now omaslack)

It never overwrites a file you changed: it keeps it and prints the diff
command to compare it with the packaged version. Running it again changes
nothing. What it installed is listed in $XDG_STATE_HOME/omaslack/setup.json.

Options:
  --window-rule  also copy the Hyprland window rule that floats the plugin's
                 window to ~/.config/hypr/omaslack.lua, and print the line
                 that loads it from hyprland.lua (setup never edits your
                 Hyprland config)
  --remove       disable the service and remove the files setup installed
                 that you did not change; credentials and data are kept
  --dry-run      print what would change, and change nothing
  --from <dir>   take the files from <dir> instead of /usr/share/omaslack;
                 a checkout's packaging/ directory works too

Exit codes:
  0  every step succeeded (warnings allowed)
  1  at least one step failed
  2  usage error
`

// errStepsFailed is what setup returns when a step failed.
var errStepsFailed = errors.New("some steps failed")

func runSetup(env Env, args []string) error {
	opts, from, remove, err := parseSetup(args)
	if errors.Is(err, flag.ErrHelp) {
		_, err = io.WriteString(env.Stdout, setupUsage)
		return err
	}
	if err != nil {
		return err
	}
	sys := setup.OS()
	if env.SetupSystem != nil {
		sys = env.SetupSystem()
	}
	layout, err := setup.DefaultLayout(sys, from)
	if err != nil {
		return err
	}
	act := setup.Install
	if remove {
		act = setup.Remove
	}
	results := act(context.Background(), sys, layout, opts)
	if err := doctor.Report(env.Stdout, results); err != nil {
		return fmt.Errorf("print the report: %w", err)
	}
	if doctor.Failed(results) {
		return errStepsFailed
	}
	return nil
}

// parseSetup reads setup's flags. --from is made absolute, so the diff
// commands setup prints work from any directory.
func parseSetup(args []string) (opts setup.Options, from string, remove bool, err error) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.WindowRule, "window-rule", false, "")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "")
	fs.BoolVar(&remove, "remove", false, "")
	fs.StringVar(&from, "from", setup.ShareDir, "")
	switch err = fs.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		return opts, "", false, err
	case err != nil:
		return opts, "", false, &usageError{err: err, usage: setupUsage}
	case fs.NArg() > 0:
		return opts, "", false, &usageError{err: fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")), usage: setupUsage}
	}
	from, err = filepath.Abs(from)
	return opts, from, remove, err
}

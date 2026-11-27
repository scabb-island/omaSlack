// Package setup is omaslack setup: the per-user part of the installation
// (spec aur-packaging, "Per-user setup" and "Setup keeps the user's
// changes"). The package installs files under /usr only; setup copies the
// theme template, the theme hook and, on request, the Hyprland window rule
// into the home directory, renders the theme once and enables the user
// service. --remove undoes it.
//
// Setup never overwrites or removes a file the user changed. It knows
// which files it installed from a manifest of their checksums
// ($XDG_STATE_HOME/omaslack/setup.json): a file whose checksum matches the
// manifest or the packaged file is setup's to update or remove; any other
// is the user's, and setup only reports it.
//
// Everything it touches goes through System, so tests run it against
// temporary directories and fake commands. Results use doctor's report
// format.
package setup

import (
	"context"
	"io/fs"

	"gitlab.com/scabb-island/omaslack/internal/doctor"
)

// The commands setup runs or suggests.
const (
	refreshTheme     = "omarchy-theme-refresh"
	enableService    = "systemctl --user enable --now omaslack"
	disableService   = "systemctl --user disable --now omaslack"
	installPackage   = "omarchy-pkg-aur-add omaslack-bin"
	daemonLog        = "journalctl --user -u omaslack -e"
	forgetWorkspaces = "omaslack auth list, then omaslack auth remove <team-id> for each workspace"
)

// requireLine is the line that loads the window rule from hyprland.lua.
// It is guarded, so removing the rule never breaks the Hyprland config.
const requireLine = `if package.searchpath("hypr.omaslack", package.path) then require("hypr.omaslack") end`

// ruleModule is how hyprland.lua names the window rule.
const ruleModule = `"hypr.omaslack"`

// FS is the filesystem setup reads and writes.
type FS interface {
	// ReadFile reads path. A missing file is an error wrapping
	// fs.ErrNotExist.
	ReadFile(path string) ([]byte, error)
	// WriteFile replaces path with data and mode, creating its parent
	// directories.
	WriteFile(path string, data []byte, mode fs.FileMode) error
	// Remove removes path.
	Remove(path string) error
}

// System is what setup works with: the filesystem, the commands it runs
// (systemctl, omarchy-theme-refresh) and the environment.
type System struct {
	FS     FS
	Exec   doctor.Output
	Getenv func(string) string
}

// Options are omaslack setup's flags.
type Options struct {
	WindowRule bool // also install the Hyprland window rule
	DryRun     bool // say what would change, and change nothing
}

// Item is one file setup installs: from the package (Source) to the home
// directory (Target).
type Item struct {
	Name   string
	Source string
	Target string
	Mode   fs.FileMode
}

// Layout is where everything setup handles lives.
type Layout struct {
	Template   Item
	Hook       Item
	WindowRule Item
	Manifest   string // what setup installed, $XDG_STATE_HOME/omaslack/setup.json
	Rendered   string // the template rendered by Omarchy for the current theme
	HyprConfig string // ~/.config/hypr/hyprland.lua, only read
	Data       string // $XDG_DATA_HOME/omaslack, only named
}

// Install installs the files, renders the theme and enables the service.
func Install(ctx context.Context, sys System, l Layout, opts Options) []doctor.Result {
	r, failed := start(sys, l, opts)
	if failed != nil {
		return []doctor.Result{*failed}
	}
	template, wrote := r.install(l.Template)
	hook, _ := r.install(l.Hook)
	results := []doctor.Result{template, hook}
	if opts.WindowRule {
		rule, _ := r.install(l.WindowRule)
		results = append(results, rule, r.ruleLoaded())
	}
	results = append(results, r.render(ctx, wrote), r.enable(ctx))
	return append(results, r.save()...)
}

// Remove disables the service and removes the files setup installed that
// the user did not change.
func Remove(ctx context.Context, sys System, l Layout, opts Options) []doctor.Result {
	r, failed := start(sys, l, opts)
	if failed != nil {
		return []doctor.Result{*failed}
	}
	results := []doctor.Result{r.disable(ctx)}
	for _, item := range []Item{l.Template, l.Hook, l.WindowRule} {
		results = append(results, r.remove(item))
	}
	if r.loadsRule() {
		results = append(results, warn("window rule loaded", "hyprland.lua still loads hypr.omaslack",
			"delete the line that loads \"hypr.omaslack\" from "+l.HyprConfig))
	}
	results = append(results, r.save()...)
	return append(results,
		ok("credentials", "kept in the keyring", forgetWorkspaces),
		ok("data", "kept in "+l.Data, "rm -rf "+l.Data))
}

func ok(check, detail, fix string) doctor.Result {
	return doctor.Result{Check: check, Status: doctor.OK, Detail: detail, Fix: fix}
}

func warn(check, detail, fix string) doctor.Result {
	return doctor.Result{Check: check, Status: doctor.Warn, Detail: detail, Fix: fix}
}

func fail(check, detail, fix string) doctor.Result {
	return doctor.Result{Check: check, Status: doctor.Fail, Detail: detail, Fix: fix}
}

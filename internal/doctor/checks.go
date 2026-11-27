package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/version"
)

// The commands doctor suggests.
const (
	startDaemon    = "systemctl --user start omaslack"
	restartDaemon  = "systemctl --user restart omaslack"
	reloadDaemon   = "systemctl --user reload omaslack"
	daemonLog      = "journalctl --user -u omaslack -e"
	installPackage = "omarchy-pkg-aur-add omaslack-bin"
	addInternalApp = "omaslack auth add --provider internal-app"
	startKeyring   = "start a Secret Service such as gnome-keyring, then " + addInternalApp + " (or desktop-session) again"
	// The package ships the theme files here (spec aur-packaging,
	// "Installed file layout"). Until `omaslack setup` (task 6.1a) copies
	// them, doctor suggests copying them by hand.
	sharedDir = "/usr/share/omaslack"
)

func checkDaemon(h Hello, err error) Result {
	const name = "daemon"
	var rpcErr *rpc.Error
	switch {
	case errors.Is(err, rpcclient.ErrUnavailable):
		return fail(name, "not running: nothing answers on its socket", startDaemon)
	case errors.As(err, &rpcErr) && rpcErr.Code == rpc.CodeIncompatibleProtocol:
		return fail(name, fmt.Sprintf("speaks another protocol than this omaslack (protocol %d)", version.Protocol), restartDaemon)
	case err != nil:
		return fail(name, err.Error(), daemonLog)
	case h.DaemonVersion != version.Version:
		return warn(name, fmt.Sprintf("runs omaslack %s, this is %s", h.DaemonVersion, version.Version), restartDaemon)
	}
	return ok(name, fmt.Sprintf("omaslack %s, protocol %d", h.DaemonVersion, h.Protocol))
}

func checkService(u Unit, err error) Result {
	const name = "service"
	switch {
	case err != nil:
		return warn(name, "cannot ask systemd: "+err.Error(), "")
	case u.Load != "loaded":
		return warn(name, "omaslack.service is not installed, so clients cannot start the daemon on demand", installPackage)
	case u.Active == "failed":
		return fail(name, "failed", daemonLog)
	case u.Active != "active":
		return warn(name, u.Active, startDaemon)
	case u.UnitFile == "enabled":
		return ok(name, "active, starts at login")
	}
	return ok(name, "active, started on demand")
}

func checkSocket(p Probes) Result {
	const name = "socket"
	path := p.Paths.Socket
	if path == "" {
		return fail(name, "XDG_RUNTIME_DIR is not set", "")
	}
	f, err := p.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return warn(name, "no socket at "+path+": the daemon is not running", startDaemon)
	case err != nil:
		return fail(name, err.Error(), "")
	}
	if problem := privacy(f, p.UID); f.Mode.Type() != fs.ModeSocket || problem != "" {
		if problem == "" {
			problem = "not a socket"
		}
		return fail(name, path+": "+problem, "rm -f "+path+" && "+restartDaemon)
	}
	return ok(name, path+" (0600, owned by you)")
}

// privacy says what makes f readable by someone else than uid, if
// anything.
func privacy(f File, uid int) string {
	if f.UID != uid {
		return fmt.Sprintf("owned by uid %d", f.UID)
	}
	if perm := f.Mode.Perm(); perm != 0o600 {
		return fmt.Sprintf("mode %04o, want 0600", perm)
	}
	return ""
}

// checkCredentials reports the keyring backend, then each workspace in it
// with its provider and, when the daemon answered, its connection state.
func checkCredentials(ctx context.Context, p Probes, h Hello, helloErr error) []Result {
	ks, err := p.Keyring(ctx)
	if err != nil {
		return []Result{fail("credentials", "cannot open the keyring: "+err.Error(), startKeyring)}
	}
	defer ks.Close()
	results := []Result{checkBackend(p, ks.Backend())}
	teams, err := ks.Teams(ctx)
	if err != nil {
		return append(results, fail("workspaces", "cannot list them: "+err.Error(), startKeyring))
	}
	if len(teams) == 0 {
		return append(results, warn("workspaces", "none", addInternalApp))
	}
	states := map[string]string{}
	for _, w := range h.Workspaces {
		states[w.Team] = w.State
	}
	for _, team := range teams {
		creds, err := ks.Get(ctx, team)
		results = append(results, checkWorkspace(team.String(), creds, err, states, helloErr == nil))
	}
	return results
}

func checkBackend(p Probes, b keyring.Backend) Result {
	const name = "credentials"
	if b != keyring.File {
		return ok(name, "Secret Service")
	}
	path := p.Paths.CredentialsFile
	if f, err := p.Stat(path); err == nil {
		if problem := privacy(f, p.UID); problem != "" {
			return fail(name, path+": "+problem, "chmod 600 "+path)
		}
	}
	return warn(name, "plain file "+path+" (no Secret Service is running)", startKeyring)
}

func checkWorkspace(team string, creds keyring.Credentials, err error, states map[string]string, daemonUp bool) Result {
	name := "workspace " + team
	if err != nil {
		return warn(name, "cannot read its credentials: "+err.Error(), addInternalApp)
	}
	provider := creds.Provider()
	detail := provider
	state, loaded := states[team]
	switch {
	case !daemonUp:
	case !loaded:
		return warn(name, provider+", not loaded by the daemon", reloadDaemon)
	case state == "auth_expired":
		return fail(name, provider+", auth_expired: Slack rejected the credentials", "omaslack auth add --provider "+provider)
	case state != "connected":
		return warn(name, provider+", "+state, daemonLog)
	default:
		detail += ", " + state
	}
	if _, unofficial := creds.(keyring.DesktopSession); unofficial {
		return warn(name, detail+": unofficial protocol, may violate Slack's terms of service",
			"switch to the official API: "+addInternalApp)
	}
	return ok(name, detail)
}

func checkTemplate(p Probes) Result {
	const name = "theme template"
	path := p.Paths.Template
	return checkInstalled(p, name, path, func(File) Result { return ok(name, path) },
		warn(name, path+" is missing", "install -Dm644 "+sharedDir+"/omaslack.toml.tpl "+path))
}

func checkHook(p Probes) Result {
	const name = "theme hook"
	path := p.Paths.Hook
	return checkInstalled(p, name, path, func(f File) Result {
		if f.Mode.Perm()&0o100 == 0 {
			return warn(name, path+" is not executable, so Omarchy skips it", "chmod +x "+path)
		}
		return ok(name, path)
	}, warn(name, path+" is missing", "install -Dm755 "+sharedDir+"/theme-set.hook "+path))
}

func checkRendered(p Probes) Result {
	const name = "theme rendered"
	path := p.Paths.Rendered
	return checkInstalled(p, name, path, func(File) Result { return ok(name, path) },
		warn(name, path+" is not rendered yet, so the TUI reads colors.toml", "omarchy-theme-refresh"))
}

// checkInstalled stats path and judges it with present, or returns missing
// when it does not exist.
func checkInstalled(p Probes, name, path string, present func(File) Result, missing Result) Result {
	if path == "" {
		return fail(name, "no home directory", "")
	}
	f, err := p.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return missing
	case err != nil:
		return fail(name, err.Error(), "")
	}
	return present(f)
}

func checkPlugin(plugins []Plugin, err error) Result {
	const name = "plugin"
	if err != nil {
		return warn(name, "cannot list the Omarchy plugins: "+err.Error(), "")
	}
	for _, pl := range plugins {
		if pl.ID != PluginID {
			continue
		}
		if !pl.Enabled {
			return warn(name, PluginID+" is installed but disabled", "omarchy plugin enable "+PluginID)
		}
		return ok(name, PluginID+" installed and enabled")
	}
	return warn(name, PluginID+" is not installed", "omarchy plugin add <omaslack plugin git URL> --enable")
}

func checkShell(s Shell, err error) Result {
	const name = "shell"
	if err != nil {
		return warn(name, "omarchy-shell not found: the bar widget and the window need Omarchy's shell", "")
	}
	detail := "running"
	if !s.Running {
		detail = "not running"
	}
	if s.Version != "" {
		detail += ", Omarchy " + s.Version
	}
	if !s.Running {
		return warn(name, detail, "omarchy restart shell")
	}
	return ok(name, detail)
}

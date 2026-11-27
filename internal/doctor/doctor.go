// Package doctor checks an omaslack installation (spec omarchy-integration,
// "Diagnostics"): the daemon and its protocol, the user service, the socket,
// where the credentials live, each workspace's provider, the Omarchy theme
// template and hook, the plugin and the shell.
//
// Every check is a pure function of what Probes read from the system, so
// the checks are tested without touching it; system.go holds the real
// probes. Nothing here prints a secret: credentials are only asked for
// their provider.
package doctor

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
)

// PluginID is the omaslack plugin's id in the Omarchy shell.
const PluginID = "ar.scabb.omaslack"

// Status is a check's verdict. Only Fail makes doctor exit non-zero.
type Status int

const (
	OK Status = iota
	Warn
	Fail
)

func (s Status) String() string { return [...]string{"OK", "WARN", "FAIL"}[s] }

// Result is one check's verdict, what it found and, unless it is OK, the
// command that fixes it (empty when there is none to suggest).
type Result struct {
	Check  string
	Status Status
	Detail string
	Fix    string
}

func ok(check, detail string) Result { return Result{check, OK, detail, ""} }

func warn(check, detail, fix string) Result { return Result{check, Warn, detail, fix} }

func fail(check, detail, fix string) Result { return Result{check, Fail, detail, fix} }

// Hello is what the daemon answers hello with.
type Hello struct {
	Protocol      int         `json:"protocol"`
	DaemonVersion string      `json:"daemonVersion"`
	Workspaces    []Workspace `json:"workspaces"`
}

// Workspace is one workspace the daemon has loaded.
type Workspace struct {
	Team     string `json:"team"`
	Provider string `json:"provider"`
	State    string `json:"state"` // domain.ConnectionState names: connected, auth_expired, ...
}

// Unit is the state of omaslack.service, as systemctl --user show
// reports it.
type Unit struct {
	Load     string // LoadState: loaded, not-found, ...
	Active   string // ActiveState: active, inactive, failed, ...
	UnitFile string // UnitFileState: enabled, disabled, static, ...
}

// File is what doctor needs from a stat.
type File struct {
	Mode fs.FileMode
	UID  int
}

// Plugin is one entry of omarchy plugin list --json.
type Plugin struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// Shell is the Omarchy shell: whether it answers and Omarchy's version.
type Shell struct {
	Running bool
	Version string // empty when unknown
}

// Paths are the files doctor looks at. An empty path means it could not be
// resolved (no home or no $XDG_RUNTIME_DIR).
type Paths struct {
	Socket          string // $XDG_RUNTIME_DIR/omaslack.sock
	Template        string // ~/.config/omarchy/themed/omaslack.toml.tpl
	Hook            string // ~/.config/omarchy/hooks/theme-set.d/omaslack
	Rendered        string // ~/.local/state/omarchy/current/theme/omaslack.toml
	CredentialsFile string // the file keyring, keyring.DefaultFilePath
}

// PathsFor resolves the socket and the Omarchy theme paths from the home
// and runtime directories. CredentialsFile is left to the caller.
func PathsFor(home, runtimeDir string) Paths {
	var p Paths
	if runtimeDir != "" {
		p.Socket = filepath.Join(runtimeDir, rpcclient.SocketName)
	}
	if home != "" {
		omarchy := filepath.Join(home, ".config", "omarchy")
		p.Template = filepath.Join(omarchy, "themed", "omaslack.toml.tpl")
		p.Hook = filepath.Join(omarchy, "hooks", "theme-set.d", "omaslack")
		rendered, _ := theme.PathsFrom(home) // home is not empty
		p.Rendered = rendered.OmaslackTOML
	}
	return p
}

// Probes read the system. Each is called once per Run.
type Probes struct {
	Paths Paths
	UID   int // the user running doctor

	// Hello connects to the daemon, without starting it, and says hello.
	Hello func(context.Context) (Hello, error)
	// Service reads omaslack.service's state.
	Service func(context.Context) (Unit, error)
	// Stat stats a path, following symlinks. A missing path is an error
	// wrapping fs.ErrNotExist.
	Stat func(path string) (File, error)
	// Keyring opens the credential store (keyring.Open).
	Keyring func(context.Context) (keyring.ClosableStore, error)
	// SessionSources lists the desktop-session sources available on this
	// machine by name (the Slack desktop app and each browser signed in to
	// Slack). Nil means the check is skipped.
	SessionSources func() []string
	// Plugins lists the Omarchy shell plugins.
	Plugins func(context.Context) ([]Plugin, error)
	// Shell finds the Omarchy shell; an error means it is not installed.
	Shell func(context.Context) (Shell, error)
}

// Run runs every check, in the order the report shows them.
func Run(ctx context.Context, p Probes) []Result {
	hello, helloErr := p.Hello(ctx)
	results := []Result{
		checkDaemon(hello, helloErr),
		checkService(p.Service(ctx)),
		checkSocket(p),
	}
	results = append(results, checkCredentials(ctx, p, hello, helloErr)...)
	results = append(results, checkSessionSources(p))
	plugins, pluginsErr := p.Plugins(ctx)
	return append(results,
		checkTemplate(p),
		checkHook(p),
		checkRendered(p),
		checkPlugin(plugins, pluginsErr),
		checkShell(p.Shell(ctx)),
	)
}

// Failed says whether any check failed.
func Failed(results []Result) bool {
	for _, r := range results {
		if r.Status == Fail {
			return true
		}
	}
	return false
}

// Report prints one line per check, the fix under it, and a summary.
func Report(w io.Writer, results []Result) error {
	var width int
	for _, r := range results {
		width = max(width, len(r.Check))
	}
	width += 3
	var b strings.Builder
	counts := [3]int{}
	for _, r := range results {
		counts[r.Status]++
		fmt.Fprintf(&b, "%-6s%-*s%s\n", r.Status, width, r.Check, r.Detail)
		if r.Fix != "" {
			fmt.Fprintf(&b, "%*sfix: %s\n", 6+width, "", r.Fix)
		}
	}
	fmt.Fprintf(&b, "\n%d OK, %s, %s\n", counts[OK], plural(counts[Warn], "warning"), plural(counts[Fail], "failure"))
	_, err := io.WriteString(w, b.String())
	return err
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

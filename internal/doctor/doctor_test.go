package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/version"
)

const me = 1000

// Every token and cookie below is fake; none may be printed.
var secrets = []string{"xoxp-user-secret", "xapp-app-secret", "xoxc-session-secret", "xoxd-cookie-secret"}

// secretService is a file store that says it is the Secret Service.
type secretService struct{ *keyring.FileStore }

func (secretService) Backend() keyring.Backend { return keyring.SecretService }

// fixture is a machine where every check passes, until a test breaks one
// part of it.
type fixture struct {
	t       *testing.T
	probes  Probes
	files   map[string]File
	store   *keyring.FileStore
	backend keyring.Backend
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, backend: keyring.SecretService}
	f.store = keyring.NewFileStore(filepath.Join(t.TempDir(), "credentials"))
	f.addTeam("T0APP", keyring.InternalApp{UserToken: "xoxp-user-secret", AppToken: "xapp-app-secret"})
	paths := PathsFor("/home/me", "/run/user/1000")
	paths.CredentialsFile = "/home/me/.config/omaslack/credentials"
	f.files = map[string]File{
		paths.Socket:   {Mode: fs.ModeSocket | 0o600, UID: me},
		paths.Template: {Mode: 0o644, UID: me},
		paths.Hook:     {Mode: 0o755, UID: me},
		paths.Rendered: {Mode: 0o644, UID: me},
	}
	f.probes = Probes{
		Paths: paths,
		UID:   me,
		Hello: func(context.Context) (Hello, error) {
			return Hello{Protocol: version.Protocol, DaemonVersion: version.Version,
				Workspaces: []Workspace{{Team: "T0APP", Provider: "internal-app", State: "connected"}}}, nil
		},
		Service: func(context.Context) (Unit, error) {
			return Unit{Load: "loaded", Active: "active", UnitFile: "enabled"}, nil
		},
		Stat: func(path string) (File, error) {
			if file, ok := f.files[path]; ok {
				return file, nil
			}
			return File{}, fmt.Errorf("stat %s: %w", path, fs.ErrNotExist)
		},
		Keyring: func(context.Context) (keyring.ClosableStore, error) {
			if f.backend == keyring.File {
				return f.store, nil
			}
			return secretService{f.store}, nil
		},
		SessionSources: func() []string { return []string{"the Slack desktop app", "Brave"} },
		Plugins: func(context.Context) ([]Plugin, error) {
			return []Plugin{{ID: "omarchy.clock", Enabled: true}, {ID: PluginID, Enabled: true}}, nil
		},
		Shell: func(context.Context) (Shell, error) {
			return Shell{Running: true, Version: "4.5.0-3"}, nil
		},
	}
	return f
}

func (f *fixture) addTeam(team string, creds keyring.Credentials) {
	f.t.Helper()
	id, err := domain.ParseTeamID(team)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.store.Set(context.Background(), id, creds); err != nil {
		f.t.Fatal(err)
	}
}

// run runs every check and returns the result of each by check name and
// the printed report, which must never hold a secret.
func (f *fixture) run() (map[string]Result, string) {
	f.t.Helper()
	results := Run(context.Background(), f.probes)
	var out bytes.Buffer
	if err := Report(&out, results); err != nil {
		f.t.Fatal(err)
	}
	for _, s := range secrets {
		if strings.Contains(out.String(), s) {
			f.t.Fatalf("the report prints a secret:\n%s", out.String())
		}
	}
	byName := map[string]Result{}
	for _, r := range results {
		byName[r.Check] = r
	}
	return byName, out.String()
}

func (f *fixture) check(name string) Result {
	f.t.Helper()
	got, out := f.run()
	r, ok := got[name]
	if !ok {
		f.t.Fatalf("no %q check in the report:\n%s", name, out)
	}
	return r
}

func want(t *testing.T, r Result, status Status, detail, fix string) {
	t.Helper()
	if r.Status != status || !strings.Contains(r.Detail, detail) || !strings.Contains(r.Fix, fix) {
		t.Errorf("%s = %v %q (fix %q), want %v with %q (fix %q)", r.Check, r.Status, r.Detail, r.Fix, status, detail, fix)
	}
	if status == OK && r.Fix != "" {
		t.Errorf("%s is OK but has a fix: %q", r.Check, r.Fix)
	}
	if status != OK && r.Fix == "" && fix != "" {
		t.Errorf("%s is %v without a fix", r.Check, r.Status)
	}
}

func TestHealthyMachinePassesEveryCheck(t *testing.T) {
	f := newFixture(t)
	results := Run(context.Background(), f.probes)
	wantChecks := []string{"daemon", "service", "socket", "credentials", "workspace T0APP",
		"session sources", "theme template", "theme hook", "theme rendered", "plugin", "shell"}
	if len(results) != len(wantChecks) {
		t.Fatalf("%d results, want %d: %+v", len(results), len(wantChecks), results)
	}
	for i, r := range results {
		if r.Check != wantChecks[i] || r.Status != OK {
			t.Errorf("result %d = %+v, want %s OK", i, r, wantChecks[i])
		}
	}
	if Failed(results) {
		t.Error("Failed = true on a healthy machine")
	}
}

func TestDaemon(t *testing.T) {
	cases := map[string]struct {
		hello       Hello
		err         error
		status      Status
		detail, fix string
	}{
		"running": {hello: Hello{Protocol: version.Protocol, DaemonVersion: version.Version},
			status: OK, detail: "omaslack " + version.Version + ", protocol 1"},
		"older build": {hello: Hello{Protocol: version.Protocol, DaemonVersion: "v0.0.1"},
			status: Warn, detail: "runs omaslack v0.0.1, this is " + version.Version, fix: "systemctl --user restart omaslack"},
		"not running": {err: fmt.Errorf("%w: refused", rpcclient.ErrUnavailable),
			status: Fail, detail: "not running", fix: "systemctl --user start omaslack"},
		"other protocol": {err: &rpc.Error{Code: rpc.CodeIncompatibleProtocol, Message: "incompatible_protocol"},
			status: Fail, detail: "another protocol", fix: "systemctl --user restart omaslack"},
		"broken": {err: errors.New("boom"), status: Fail, detail: "boom", fix: "journalctl --user -u omaslack"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.probes.Hello = func(context.Context) (Hello, error) { return c.hello, c.err }
			want(t, f.check("daemon"), c.status, c.detail, c.fix)
		})
	}
}

func TestService(t *testing.T) {
	cases := map[string]struct {
		unit        Unit
		err         error
		status      Status
		detail, fix string
	}{
		"enabled":   {unit: Unit{Load: "loaded", Active: "active", UnitFile: "enabled"}, status: OK, detail: "active, starts at login"},
		"on demand": {unit: Unit{Load: "loaded", Active: "active", UnitFile: "disabled"}, status: OK, detail: "active, started on demand"},
		"stopped": {unit: Unit{Load: "loaded", Active: "inactive", UnitFile: "disabled"},
			status: Warn, detail: "inactive", fix: "systemctl --user start omaslack"},
		"crashed": {unit: Unit{Load: "loaded", Active: "failed", UnitFile: "enabled"},
			status: Fail, detail: "failed", fix: "journalctl --user -u omaslack"},
		"not installed": {unit: Unit{Load: "not-found", Active: "inactive"},
			status: Warn, detail: "not installed", fix: "omarchy-pkg-aur-add omaslack-bin"},
		"no systemd": {err: errors.New("no user bus"), status: Warn, detail: "no user bus"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.probes.Service = func(context.Context) (Unit, error) { return c.unit, c.err }
			want(t, f.check("service"), c.status, c.detail, c.fix)
		})
	}
}

func TestSocket(t *testing.T) {
	sock := "/run/user/1000/omaslack.sock"
	recreate := "rm -f " + sock + " && systemctl --user restart omaslack"
	cases := map[string]struct {
		file        *File // nil: missing
		status      Status
		detail, fix string
	}{
		"private":    {file: &File{Mode: fs.ModeSocket | 0o600, UID: me}, status: OK, detail: sock + " (0600, owned by you)"},
		"missing":    {status: Warn, detail: "no socket", fix: "systemctl --user start omaslack"},
		"open":       {file: &File{Mode: fs.ModeSocket | 0o666, UID: me}, status: Fail, detail: "mode 0666, want 0600", fix: recreate},
		"someone's":  {file: &File{Mode: fs.ModeSocket | 0o600, UID: 0}, status: Fail, detail: "owned by uid 0", fix: recreate},
		"not socket": {file: &File{Mode: 0o600, UID: me}, status: Fail, detail: "not a socket", fix: recreate},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			delete(f.files, sock)
			if c.file != nil {
				f.files[sock] = *c.file
			}
			want(t, f.check("socket"), c.status, c.detail, c.fix)
		})
	}
}

func TestSocketWithoutRuntimeDir(t *testing.T) {
	f := newFixture(t)
	f.probes.Paths.Socket = ""
	want(t, f.check("socket"), Fail, "XDG_RUNTIME_DIR is not set", "")
}

func TestStatErrorIsReported(t *testing.T) {
	f := newFixture(t)
	f.probes.Stat = func(string) (File, error) { return File{}, fs.ErrPermission }
	want(t, f.check("socket"), Fail, "permission denied", "")
	want(t, f.check("theme hook"), Fail, "permission denied", "")
}

func TestCredentials(t *testing.T) {
	cred := "/home/me/.config/omaslack/credentials"
	cases := map[string]struct {
		backend     keyring.Backend
		file        *File
		status      Status
		detail, fix string
	}{
		"secret service": {backend: keyring.SecretService, status: OK, detail: "Secret Service"},
		"private file": {backend: keyring.File, file: &File{Mode: 0o600, UID: me},
			status: Warn, detail: "plain file " + cred, fix: "gnome-keyring"},
		"open file": {backend: keyring.File, file: &File{Mode: 0o644, UID: me},
			status: Fail, detail: "mode 0644, want 0600", fix: "chmod 600 " + cred},
		"no file yet": {backend: keyring.File, status: Warn, detail: "plain file " + cred, fix: "gnome-keyring"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.backend = c.backend
			if c.file != nil {
				f.files[cred] = *c.file
			}
			want(t, f.check("credentials"), c.status, c.detail, c.fix)
		})
	}
}

func TestKeyringThatDoesNotOpen(t *testing.T) {
	f := newFixture(t)
	f.probes.Keyring = func(context.Context) (keyring.ClosableStore, error) { return nil, errors.New("locked") }
	got, _ := f.run()
	want(t, got["credentials"], Fail, "locked", "gnome-keyring")
	for name := range got {
		if strings.HasPrefix(name, "workspace") {
			t.Errorf("a keyring that does not open lists %q", name)
		}
	}
}

func TestNoWorkspaces(t *testing.T) {
	f := newFixture(t)
	f.store = keyring.NewFileStore(filepath.Join(t.TempDir(), "credentials"))
	want(t, f.check("workspaces"), Warn, "none", "omaslack auth add --provider internal-app")
}

func TestWorkspaces(t *testing.T) {
	cases := map[string]struct {
		creds       keyring.Credentials
		state       string // what the daemon says; "" when it does not list the team
		status      Status
		detail, fix string
	}{
		"internal app": {creds: keyring.InternalApp{UserToken: "xoxp-user-secret"}, state: "connected",
			status: OK, detail: "internal-app, connected"},
		"desktop session": {creds: keyring.DesktopSession{Token: "xoxc-session-secret", Cookie: "xoxd-cookie-secret"}, state: "connected",
			status: Warn, detail: "desktop-session, connected: unofficial protocol, may violate Slack's terms of service",
			fix: "omaslack auth add --provider internal-app"},
		"expired": {creds: keyring.InternalApp{UserToken: "xoxp-user-secret"}, state: "auth_expired",
			status: Fail, detail: "internal-app, auth_expired", fix: "omaslack auth add --provider internal-app"},
		"expired session": {creds: keyring.DesktopSession{Token: "xoxc-session-secret"}, state: "auth_expired",
			status: Fail, detail: "desktop-session, auth_expired", fix: "omaslack auth add --provider desktop-session"},
		"reconnecting": {creds: keyring.InternalApp{UserToken: "xoxp-user-secret"}, state: "disconnected",
			status: Warn, detail: "internal-app, disconnected", fix: "journalctl --user -u omaslack"},
		"not loaded": {creds: keyring.InternalApp{UserToken: "xoxp-user-secret"},
			status: Warn, detail: "internal-app, not loaded by the daemon", fix: "systemctl --user reload omaslack"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.store = keyring.NewFileStore(filepath.Join(t.TempDir(), "credentials"))
			f.addTeam("T0WS", c.creds)
			f.probes.Hello = func(context.Context) (Hello, error) {
				h := Hello{Protocol: version.Protocol, DaemonVersion: version.Version}
				if c.state != "" {
					h.Workspaces = []Workspace{{Team: "T0WS", State: c.state}}
				}
				return h, nil
			}
			want(t, f.check("workspace T0WS"), c.status, c.detail, c.fix)
		})
	}
}

func TestWorkspaceStateIsSkippedWithoutDaemon(t *testing.T) {
	f := newFixture(t)
	f.probes.Hello = func(context.Context) (Hello, error) { return Hello{}, rpcclient.ErrUnavailable }
	want(t, f.check("workspace T0APP"), OK, "internal-app", "")
}

func TestSessionSources(t *testing.T) {
	t.Run("lists the available sources", func(t *testing.T) {
		f := newFixture(t)
		want(t, f.check("session sources"), OK, "the Slack desktop app, Brave", "")
	})
	t.Run("warns when none is signed in", func(t *testing.T) {
		f := newFixture(t)
		f.probes.SessionSources = func() []string { return nil }
		want(t, f.check("session sources"), Warn, "no Slack session", "auth add --provider desktop-session")
	})
	t.Run("skipped when the probe is absent", func(t *testing.T) {
		f := newFixture(t)
		f.probes.SessionSources = nil
		want(t, f.check("session sources"), OK, "not checked", "")
	})
}

func TestTheme(t *testing.T) {
	paths := PathsFor("/home/me", "/run/user/1000")
	cases := map[string]struct {
		check, path string
		file        *File
		status      Status
		detail, fix string
	}{
		"missing template": {check: "theme template", path: paths.Template, status: Warn, detail: "missing",
			fix: "install -Dm644 /usr/share/omaslack/omaslack.toml.tpl " + paths.Template},
		"missing hook": {check: "theme hook", path: paths.Hook, status: Warn, detail: "missing",
			fix: "install -Dm755 /usr/share/omaslack/theme-set.hook " + paths.Hook},
		"hook not executable": {check: "theme hook", path: paths.Hook, file: &File{Mode: 0o644, UID: me},
			status: Warn, detail: "not executable", fix: "chmod +x " + paths.Hook},
		"not rendered": {check: "theme rendered", path: paths.Rendered, status: Warn,
			detail: "not rendered yet", fix: "omarchy-theme-refresh"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			delete(f.files, c.path)
			if c.file != nil {
				f.files[c.path] = *c.file
			}
			want(t, f.check(c.check), c.status, c.detail, c.fix)
		})
	}
}

func TestPathsFor(t *testing.T) {
	got := PathsFor("/home/me", "/run/user/1000")
	wantPaths := Paths{
		Socket:   "/run/user/1000/omaslack.sock",
		Template: "/home/me/.config/omarchy/themed/omaslack.toml.tpl",
		Hook:     "/home/me/.config/omarchy/hooks/theme-set.d/omaslack",
		Rendered: "/home/me/.local/state/omarchy/current/theme/omaslack.toml",
	}
	if got != wantPaths {
		t.Errorf("PathsFor = %+v, want %+v", got, wantPaths)
	}
	if got := PathsFor("", ""); got != (Paths{}) {
		t.Errorf("PathsFor without home or runtime dir = %+v, want no paths", got)
	}
}

func TestThemeWithoutHome(t *testing.T) {
	f := newFixture(t)
	f.probes.Paths.Template = ""
	want(t, f.check("theme template"), Fail, "no home directory", "")
}

func TestPlugin(t *testing.T) {
	cases := map[string]struct {
		plugins     []Plugin
		err         error
		status      Status
		detail, fix string
	}{
		"enabled":  {plugins: []Plugin{{ID: PluginID, Enabled: true}}, status: OK, detail: "installed and enabled"},
		"disabled": {plugins: []Plugin{{ID: PluginID}}, status: Warn, detail: "disabled", fix: "omarchy plugin enable " + PluginID},
		"missing":  {plugins: []Plugin{{ID: "omarchy.clock", Enabled: true}}, status: Warn, detail: "not installed", fix: "omarchy plugin add"},
		"no omarchy": {err: errors.New(`exec: "omarchy": executable file not found`),
			status: Warn, detail: "cannot list the Omarchy plugins"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.probes.Plugins = func(context.Context) ([]Plugin, error) { return c.plugins, c.err }
			want(t, f.check("plugin"), c.status, c.detail, c.fix)
		})
	}
}

func TestShell(t *testing.T) {
	cases := map[string]struct {
		shell       Shell
		err         error
		status      Status
		detail, fix string
	}{
		"running":     {shell: Shell{Running: true, Version: "4.5.0-3"}, status: OK, detail: "running, Omarchy 4.5.0-3"},
		"unversioned": {shell: Shell{Running: true}, status: OK, detail: "running"},
		"stopped":     {shell: Shell{Version: "4.5.0-3"}, status: Warn, detail: "not running", fix: "omarchy restart shell"},
		"missing":     {err: errors.New("not found"), status: Warn, detail: "omarchy-shell not found"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.probes.Shell = func(context.Context) (Shell, error) { return c.shell, c.err }
			want(t, f.check("shell"), c.status, c.detail, c.fix)
		})
	}
}

func TestFailedOnlyOnFail(t *testing.T) {
	if Failed([]Result{{Status: OK}, {Status: Warn}}) {
		t.Error("warnings are not failures")
	}
	if !Failed([]Result{{Status: OK}, {Status: Fail}}) {
		t.Error("a FAIL is a failure")
	}
}

func TestReport(t *testing.T) {
	var out bytes.Buffer
	err := Report(&out, []Result{
		{Check: "daemon", Status: OK, Detail: "omaslack v1, protocol 1"},
		{Check: "theme hook", Status: Warn, Detail: "missing", Fix: "install it"},
		{Check: "socket", Status: Fail, Detail: "mode 0666", Fix: "recreate it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOut := `OK    daemon       omaslack v1, protocol 1
WARN  theme hook   missing
                   fix: install it
FAIL  socket       mode 0666
                   fix: recreate it

1 OK, 1 warning, 1 failure
`
	if out.String() != wantOut {
		t.Errorf("report:\n%s\nwant:\n%s", out.String(), wantOut)
	}
}

// brokenStore lists T0APP but fails what failing names: "teams" or "get".
type brokenStore struct {
	secretService
	failing string
}

func (s brokenStore) Teams(ctx context.Context) ([]domain.TeamID, error) {
	if s.failing == "teams" {
		return nil, errors.New("collection locked")
	}
	return s.secretService.Teams(ctx)
}

func (s brokenStore) Get(context.Context, domain.TeamID) (keyring.Credentials, error) {
	return nil, errors.New("item locked")
}

func TestKeyringThatFailsToRead(t *testing.T) {
	for failing, check := range map[string]Result{
		"teams": {Check: "workspaces", Status: Fail, Detail: "collection locked", Fix: "gnome-keyring"},
		"get":   {Check: "workspace T0APP", Status: Warn, Detail: "item locked", Fix: "omaslack auth add"},
	} {
		t.Run(failing, func(t *testing.T) {
			f := newFixture(t)
			f.probes.Keyring = func(context.Context) (keyring.ClosableStore, error) {
				return brokenStore{secretService{f.store}, failing}, nil
			}
			want(t, f.check(check.Check), check.Status, check.Detail, check.Fix)
		})
	}
}

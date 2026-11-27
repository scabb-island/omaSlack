package setup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/doctor"
)

// The packaged files, as /usr/share/omaslack holds them.
const (
	templateV1 = "background = \"{{ background }}\"\n"
	templateV2 = "background = \"{{ background }}\"\naccent = \"{{ accent }}\"\n"
	hookV1     = "#!/bin/bash\nexit 0\n"
	ruleV1     = "o.window({ title = \"^omaslack$\" }, { float = true })\n"
)

// Unit states as systemctl --user show prints them.
const (
	unitMissing  = "LoadState=not-found\nActiveState=inactive\nUnitFileState=\n"
	unitDisabled = "LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n"
	unitEnabled  = "LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n"
)

// fakeExec answers the commands setup runs and records them.
// systemctl show prints unit; omarchy-theme-refresh renders the template
// to rendered, like Omarchy does when a theme is set.
type fakeExec struct {
	unit     string
	rendered string
	calls    []string
	fail     map[string]error
}

func (f *fakeExec) run(_ context.Context, name string, args ...string) (string, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, cmd)
	if err := f.fail[cmd]; err != nil {
		return "", err
	}
	switch {
	case strings.HasPrefix(cmd, "systemctl --user show omaslack"):
		return f.unit, nil
	case cmd == "systemctl --user enable --now omaslack":
		f.unit = unitEnabled
	case cmd == "systemctl --user disable --now omaslack":
		f.unit = unitDisabled
	case cmd == "omarchy-theme-refresh" && f.rendered != "":
		return "", writeFile(f.rendered, "rendered", 0o644)
	}
	return "", nil
}

// mutating lists the calls that change something: all but systemctl show.
func (f *fakeExec) mutating() []string {
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "systemctl --user show") {
			out = append(out, c)
		}
	}
	return out
}

// machine is a temporary home and share directory with the packaged files.
type machine struct {
	root   string
	share  string
	home   string
	exec   *fakeExec
	env    map[string]string
	layout Layout
	sys    System
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	root := t.TempDir()
	m := &machine{root: root, share: filepath.Join(root, "share"), home: filepath.Join(root, "home")}
	m.packaged(t, "omaslack.toml.tpl", templateV1)
	m.packaged(t, "theme-set.hook", hookV1)
	m.packaged(t, "hypr/omaslack.lua", ruleV1)
	m.env = map[string]string{"HOME": m.home}
	m.exec = &fakeExec{unit: unitDisabled}
	m.sys = System{FS: OSFS{}, Exec: m.exec.run, Getenv: func(k string) string { return m.env[k] }}
	layout, err := DefaultLayout(m.sys, m.share)
	if err != nil {
		t.Fatal(err)
	}
	m.layout = layout
	m.exec.rendered = layout.Rendered
	return m
}

func (m *machine) packaged(t *testing.T, name, content string) {
	t.Helper()
	if err := writeFile(filepath.Join(m.share, name), content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (m *machine) install(opts Options) []doctor.Result {
	return Install(context.Background(), m.sys, m.layout, opts)
}

func (m *machine) remove(opts Options) []doctor.Result {
	return Remove(context.Background(), m.sys, m.layout, opts)
}

func writeFile(path, content string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func missing(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// tree is every file under root with its mode and content.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files[path] = info.Mode().String()
		if d.Type().IsRegular() {
			files[path] += " " + read(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// result finds the result of check, failing the test when there is none.
func result(t *testing.T, results []doctor.Result, check string) doctor.Result {
	t.Helper()
	for _, r := range results {
		if r.Check == check {
			return r
		}
	}
	t.Fatalf("no %q result in %+v", check, results)
	return doctor.Result{}
}

func expect(t *testing.T, r doctor.Result, status doctor.Status, detail, fix string) {
	t.Helper()
	if r.Status != status || !strings.Contains(r.Detail, detail) || !strings.Contains(r.Fix, fix) {
		t.Errorf("%s = %v %q (fix %q), want %v containing %q (fix containing %q)", r.Check, r.Status, r.Detail, r.Fix, status, detail, fix)
	}
}

func TestInstallOnAFreshHome(t *testing.T) {
	m := newMachine(t)
	results := m.install(Options{})

	l := m.layout
	if got := read(t, l.Template.Target); got != templateV1 {
		t.Errorf("template = %q", got)
	}
	if got := mode(t, l.Template.Target); got != 0o644 {
		t.Errorf("template mode = %v, want 0644", got)
	}
	if got := mode(t, l.Hook.Target); got != 0o755 {
		t.Errorf("hook mode = %v, want 0755", got)
	}
	if !missing(l.WindowRule.Target) {
		t.Error("the window rule was installed without --window-rule")
	}
	want := []string{"omarchy-theme-refresh", "systemctl --user enable --now omaslack"}
	if got := m.exec.mutating(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("ran %q, want %q", got, want)
	}
	expect(t, result(t, results, "theme template"), doctor.OK, "installed "+l.Template.Target, "")
	expect(t, result(t, results, "theme hook"), doctor.OK, "installed "+l.Hook.Target, "")
	expect(t, result(t, results, "theme rendered"), doctor.OK, "rendered "+l.Rendered, "")
	expect(t, result(t, results, "service"), doctor.OK, "enabled and started", "")
	if doctor.Failed(results) {
		t.Errorf("failed: %+v", results)
	}
}

func TestSecondRunChangesNothing(t *testing.T) {
	m := newMachine(t)
	m.install(Options{WindowRule: true})
	before := tree(t, m.root)
	m.exec.calls = nil

	results := m.install(Options{WindowRule: true})

	if after := tree(t, m.root); !equal(before, after) {
		t.Errorf("the second run changed files:\nbefore %v\nafter  %v", before, after)
	}
	if got := m.exec.mutating(); len(got) > 0 {
		t.Errorf("the second run ran %q", got)
	}
	for _, check := range []string{"theme template", "theme hook", "window rule"} {
		expect(t, result(t, results, check), doctor.OK, "already in place", "")
	}
	expect(t, result(t, results, "theme rendered"), doctor.OK, "already rendered", "")
	expect(t, result(t, results, "service"), doctor.OK, "already enabled and running", "")
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestInstallKeepsAnEditedTemplate(t *testing.T) {
	m := newMachine(t)
	edited := "background = \"#000000\"\n"
	if err := writeFile(m.layout.Template.Target, edited, 0o644); err != nil {
		t.Fatal(err)
	}

	results := m.install(Options{})

	if got := read(t, m.layout.Template.Target); got != edited {
		t.Errorf("the edited template was overwritten: %q", got)
	}
	expect(t, result(t, results, "theme template"), doctor.Warn, "newer version",
		"diff -u "+m.layout.Template.Target+" "+m.layout.Template.Source)
	if doctor.Failed(results) {
		t.Errorf("an edited template failed setup: %+v", results)
	}
}

func TestInstallUpdatesAnUnchangedFileToTheNewPackage(t *testing.T) {
	m := newMachine(t)
	m.install(Options{})
	m.packaged(t, "omaslack.toml.tpl", templateV2)
	m.exec.calls = nil

	results := m.install(Options{})

	if got := read(t, m.layout.Template.Target); got != templateV2 {
		t.Errorf("template = %q, want the new packaged version", got)
	}
	expect(t, result(t, results, "theme template"), doctor.OK, "updated", "")
	if got := m.exec.mutating(); len(got) != 1 || got[0] != "omarchy-theme-refresh" {
		t.Errorf("ran %q, want only the theme refresh", got)
	}
}

func TestInstallKeepsAnEditedFileAfterAPackageUpdate(t *testing.T) {
	m := newMachine(t)
	m.install(Options{})
	edited := templateV1 + "# mine\n"
	if err := writeFile(m.layout.Template.Target, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	m.packaged(t, "omaslack.toml.tpl", templateV2)

	results := m.install(Options{})

	if got := read(t, m.layout.Template.Target); got != edited {
		t.Errorf("template = %q, want the user's", got)
	}
	expect(t, result(t, results, "theme template"), doctor.Warn, "newer version", "diff -u")
}

func TestDryRunWritesNothing(t *testing.T) {
	for name, prepare := range map[string]func(*machine){
		"fresh home":     func(*machine) {},
		"package update": func(m *machine) { m.install(Options{WindowRule: true}); m.packaged(t, "omaslack.toml.tpl", templateV2) },
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			prepare(m)
			before := tree(t, m.root)
			m.exec.calls = nil

			results := m.install(Options{WindowRule: true, DryRun: true})

			if after := tree(t, m.root); !equal(before, after) {
				t.Errorf("the dry run changed files:\nbefore %v\nafter  %v", before, after)
			}
			if got := m.exec.mutating(); len(got) > 0 {
				t.Errorf("the dry run ran %q", got)
			}
			expect(t, result(t, results, "theme template"), doctor.OK, "would ", "")
		})
	}
}

func TestDryRunSaysWhatItWouldDo(t *testing.T) {
	m := newMachine(t)

	results := m.install(Options{WindowRule: true, DryRun: true})

	expect(t, result(t, results, "theme template"), doctor.OK, "would install "+m.layout.Template.Target, "")
	expect(t, result(t, results, "window rule"), doctor.OK, "would install "+m.layout.WindowRule.Target, "")
	expect(t, result(t, results, "theme rendered"), doctor.OK, "would run omarchy-theme-refresh", "")
	expect(t, result(t, results, "service"), doctor.OK, "would run systemctl --user enable --now omaslack", "")
}

func TestWindowRule(t *testing.T) {
	line := `require("hypr.omaslack")`
	for name, tc := range map[string]struct {
		config string
		status doctor.Status
		detail string
		fix    string
	}{
		"not loaded": {config: "require(\"default.hypr.omarchy\")\n", status: doctor.Warn,
			detail: "Hyprland does not load it yet", fix: line},
		"no config": {status: doctor.Warn, detail: "does not load it", fix: line},
		"loaded": {config: "require(\"default.hypr.omarchy\")\n" + requireLine + "\n", status: doctor.OK,
			detail: "hyprland.lua loads it"},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			if tc.config != "" {
				if err := writeFile(m.layout.HyprConfig, tc.config, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before := tc.config

			results := m.install(Options{WindowRule: true})

			if got := read(t, m.layout.WindowRule.Target); got != ruleV1 {
				t.Errorf("rule = %q", got)
			}
			if tc.config != "" && read(t, m.layout.HyprConfig) != before {
				t.Error("setup edited hyprland.lua")
			}
			expect(t, result(t, results, "window rule loaded"), tc.status, tc.detail, tc.fix)
		})
	}
}

func TestServiceNotInstalled(t *testing.T) {
	m := newMachine(t)
	m.exec.unit = unitMissing

	results := m.install(Options{})

	expect(t, result(t, results, "service"), doctor.Warn, "not installed", "omarchy-pkg-aur-add omaslack-bin")
	for _, c := range m.exec.calls {
		if strings.Contains(c, "enable") {
			t.Errorf("ran %q for a unit that does not exist", c)
		}
	}
}

func TestFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare func(*machine)
		check   string
		status  doctor.Status
		detail  string
	}{
		"package missing": {
			prepare: func(m *machine) { _ = os.Remove(filepath.Join(m.share, "theme-set.hook")) },
			check:   "theme hook", status: doctor.Fail, detail: "cannot read the packaged file",
		},
		"enable fails": {
			prepare: func(m *machine) {
				m.exec.fail = map[string]error{"systemctl --user enable --now omaslack": errors.New("exit status 1")}
			},
			check: "service", status: doctor.Fail, detail: "exit status 1",
		},
		"refresh fails": {
			prepare: func(m *machine) { m.exec.fail = map[string]error{"omarchy-theme-refresh": errors.New("not found")} },
			check:   "theme rendered", status: doctor.Warn, detail: "not found",
		},
		"no current theme": {
			prepare: func(m *machine) { m.exec.rendered = "" },
			check:   "theme rendered", status: doctor.Warn, detail: "was not rendered",
		},
		"systemd unreachable": {
			prepare: func(m *machine) {
				m.exec.fail = map[string]error{"systemctl --user show omaslack --property=LoadState,ActiveState,UnitFileState": errors.New("no bus")}
			},
			check: "service", status: doctor.Warn, detail: "no bus",
		},
		"corrupt manifest": {
			prepare: func(m *machine) { _ = writeFile(m.layout.Manifest, "{", 0o600) },
			check:   "manifest", status: doctor.Fail, detail: "cannot read",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			tc.prepare(m)
			results := m.install(Options{})
			expect(t, result(t, results, tc.check), tc.status, tc.detail, "")
		})
	}
}

func TestWriteFailureIsReported(t *testing.T) {
	m := newMachine(t)
	m.sys.FS = failingWrites{OSFS{}}

	results := m.install(Options{})

	expect(t, result(t, results, "theme template"), doctor.Fail, "disk full", "")
	if !doctor.Failed(results) {
		t.Error("a failed write did not fail setup")
	}
}

type failingWrites struct{ OSFS }

func (failingWrites) WriteFile(string, []byte, fs.FileMode) error { return errors.New("disk full") }

func TestRemove(t *testing.T) {
	m := newMachine(t)
	m.install(Options{WindowRule: true})
	edited := hookV1 + "# mine\n"
	if err := writeFile(m.layout.Hook.Target, edited, 0o755); err != nil {
		t.Fatal(err)
	}
	m.exec.calls = nil

	results := m.remove(Options{})

	if !missing(m.layout.Template.Target) || !missing(m.layout.WindowRule.Target) {
		t.Error("the unchanged template or window rule is still there")
	}
	if got := read(t, m.layout.Hook.Target); got != edited {
		t.Errorf("the edited hook was removed or changed: %q", got)
	}
	if got := m.exec.mutating(); len(got) != 1 || got[0] != "systemctl --user disable --now omaslack" {
		t.Errorf("ran %q, want only the disable", got)
	}
	expect(t, result(t, results, "service"), doctor.OK, "disabled and stopped", "")
	expect(t, result(t, results, "theme template"), doctor.OK, "removed "+m.layout.Template.Target, "")
	expect(t, result(t, results, "theme hook"), doctor.Warn, "kept", "rm "+m.layout.Hook.Target)
	expect(t, result(t, results, "credentials"), doctor.OK, "kept", "omaslack auth remove")
	expect(t, result(t, results, "data"), doctor.OK, filepath.Join(m.home, ".local", "share", "omaslack"), "rm -rf")
}

func TestRemoveForgetsWhatItRemoved(t *testing.T) {
	m := newMachine(t)
	m.install(Options{})
	m.remove(Options{})
	if !missing(m.layout.Manifest) {
		t.Errorf("the manifest is left behind with nothing in it: %q", read(t, m.layout.Manifest))
	}

	// A file the user creates later with the old content of another
	// version is not setup's to remove.
	if err := writeFile(m.layout.Template.Target, "mine\n", 0o644); err != nil {
		t.Fatal(err)
	}
	results := m.remove(Options{})
	expect(t, result(t, results, "theme template"), doctor.Warn, "kept", "")
}

func TestRemoveAfterThePackageIsGone(t *testing.T) {
	m := newMachine(t)
	m.install(Options{})
	if err := os.RemoveAll(m.share); err != nil {
		t.Fatal(err)
	}

	results := m.remove(Options{})

	expect(t, result(t, results, "theme template"), doctor.OK, "removed", "")
	expect(t, result(t, results, "theme hook"), doctor.OK, "removed", "")
	expect(t, result(t, results, "window rule"), doctor.OK, "not installed", "")
}

func TestRemoveWarnsWhenHyprlandStillLoadsTheRule(t *testing.T) {
	m := newMachine(t)
	m.install(Options{WindowRule: true})
	if err := writeFile(m.layout.HyprConfig, requireLine+"\n", 0o644); err != nil {
		t.Fatal(err)
	}

	results := m.remove(Options{})

	expect(t, result(t, results, "window rule loaded"), doctor.Warn, "still", "hypr.omaslack")
}

func TestRemoveDryRunWritesNothing(t *testing.T) {
	m := newMachine(t)
	m.exec.unit = unitEnabled
	m.install(Options{WindowRule: true})
	before := tree(t, m.root)
	m.exec.calls = nil

	results := m.remove(Options{DryRun: true})

	if after := tree(t, m.root); !equal(before, after) {
		t.Errorf("the dry run changed files:\nbefore %v\nafter  %v", before, after)
	}
	if got := m.exec.mutating(); len(got) > 0 {
		t.Errorf("the dry run ran %q", got)
	}
	expect(t, result(t, results, "theme template"), doctor.OK, "would remove", "")
	expect(t, result(t, results, "service"), doctor.OK, "would run systemctl --user disable --now omaslack", "")
}

func TestRemoveWhenNothingIsInstalled(t *testing.T) {
	m := newMachine(t)
	m.exec.unit = unitMissing

	results := m.remove(Options{})

	expect(t, result(t, results, "service"), doctor.OK, "not installed", "")
	expect(t, result(t, results, "theme template"), doctor.OK, "not installed", "")
	if got := m.exec.mutating(); len(got) > 0 {
		t.Errorf("ran %q", got)
	}
}

func TestLayout(t *testing.T) {
	env := map[string]string{"HOME": "/home/me"}
	sys := System{FS: OSFS{}, Getenv: func(k string) string { return env[k] }}

	l, err := DefaultLayout(sys, "/usr/share/omaslack")
	if err != nil {
		t.Fatal(err)
	}
	for got, want := range map[string]string{
		l.Template.Source:   "/usr/share/omaslack/omaslack.toml.tpl",
		l.Template.Target:   "/home/me/.config/omarchy/themed/omaslack.toml.tpl",
		l.Hook.Source:       "/usr/share/omaslack/theme-set.hook",
		l.Hook.Target:       "/home/me/.config/omarchy/hooks/theme-set.d/omaslack",
		l.WindowRule.Source: "/usr/share/omaslack/hypr/omaslack.lua",
		l.WindowRule.Target: "/home/me/.config/hypr/omaslack.lua",
		l.HyprConfig:        "/home/me/.config/hypr/hyprland.lua",
		l.Rendered:          "/home/me/.local/state/omarchy/current/theme/omaslack.toml",
		l.Manifest:          "/home/me/.local/state/omaslack/setup.json",
		l.Data:              "/home/me/.local/share/omaslack",
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}

	env["XDG_STATE_HOME"], env["XDG_DATA_HOME"] = "/state", "/data"
	l, _ = DefaultLayout(sys, "/usr/share/omaslack")
	if l.Manifest != "/state/omaslack/setup.json" || l.Data != "/data/omaslack" {
		t.Errorf("XDG paths ignored: %s, %s", l.Manifest, l.Data)
	}

	delete(env, "HOME")
	if _, err := DefaultLayout(sys, "/usr/share/omaslack"); err == nil {
		t.Error("no error without a home")
	}
}

// A checkout's packaging/ directory works as the source, for development.
func TestLayoutFromACheckout(t *testing.T) {
	packaging, err := filepath.Abs("../../packaging")
	if err != nil {
		t.Fatal(err)
	}
	sys := System{FS: OSFS{}, Getenv: func(k string) string { return map[string]string{"HOME": "/home/me"}[k] }}

	l, err := DefaultLayout(sys, packaging)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []Item{l.Template, l.Hook, l.WindowRule} {
		if _, err := os.Stat(item.Source); err != nil {
			t.Errorf("%s: %v", item.Name, err)
		}
	}
}

// The real commands' errors carry what they printed, so systemctl's own
// message reaches the report.
func TestOutputCarriesTheCommandsMessage(t *testing.T) {
	out, err := OS().Exec(context.Background(), "sh", "-c", "echo fine")
	if err != nil || out != "fine\n" {
		t.Errorf("out=%q err=%v", out, err)
	}
	_, err = OS().Exec(context.Background(), "sh", "-c", "echo Unit omaslack.service not found. >&2; exit 1")
	if err == nil || !strings.Contains(err.Error(), "Unit omaslack.service not found.") {
		t.Errorf("err = %v, want the command's message", err)
	}
}

// The service is enabled and started unless it already is both.
func TestServiceStates(t *testing.T) {
	enabledStopped := "LoadState=loaded\nActiveState=inactive\nUnitFileState=enabled\n"
	runningDisabled := "LoadState=loaded\nActiveState=active\nUnitFileState=disabled\n"
	for name, tc := range map[string]struct {
		unit, install, remove string
	}{
		"enabled, stopped":  {unit: enabledStopped, install: enableService, remove: disableService},
		"running, disabled": {unit: runningDisabled, install: enableService, remove: disableService},
		"disabled, stopped": {unit: unitDisabled, install: enableService},
		"enabled, running":  {unit: unitEnabled, remove: disableService},
	} {
		t.Run(name, func(t *testing.T) {
			for _, step := range []struct {
				act  func(*machine) []doctor.Result
				want string
			}{
				{func(m *machine) []doctor.Result { return m.install(Options{}) }, tc.install},
				{func(m *machine) []doctor.Result { return m.remove(Options{}) }, tc.remove},
			} {
				m := newMachine(t)
				m.exec.unit = tc.unit
				step.act(m)
				var systemctl []string
				for _, c := range m.exec.mutating() {
					if strings.HasPrefix(c, "systemctl") {
						systemctl = append(systemctl, c)
					}
				}
				if got := strings.Join(systemctl, "|"); got != step.want {
					t.Errorf("ran %q, want %q", got, step.want)
				}
			}
		})
	}
}

// The theme is rendered again only when setup changed the template, or it
// was never rendered.
func TestRenderOnlyWhenTheTemplateChanged(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare func(*testing.T, *machine)
		opts    Options
		detail  string
	}{
		"kept edited template": {
			prepare: func(t *testing.T, m *machine) { mustWrite(t, m.layout.Template.Target, "mine\n") },
			detail:  "already rendered",
		},
		"packaged template missing": {
			prepare: func(t *testing.T, m *machine) { _ = os.Remove(m.layout.Template.Source) },
			detail:  "already rendered",
		},
		"failed write": {
			prepare: func(t *testing.T, m *machine) { m.sys.FS = failingWrites{OSFS{}} },
			detail:  "already rendered",
		},
		"dry run of an update": {
			prepare: func(t *testing.T, m *machine) {
				m.install(Options{})
				m.packaged(t, "omaslack.toml.tpl", templateV2)
			},
			opts:   Options{DryRun: true},
			detail: "would run omarchy-theme-refresh",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMachine(t)
			m.exec.unit = unitEnabled
			mustWrite(t, m.layout.Rendered, "rendered")
			tc.prepare(t, m)
			m.exec.calls = nil

			results := m.install(tc.opts)

			expect(t, result(t, results, "theme rendered"), doctor.OK, tc.detail, "")
			if got := m.exec.mutating(); len(got) > 0 {
				t.Errorf("ran %q", got)
			}
		})
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := writeFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A file that is exactly the packaged one is removed even when setup did
// not install it (it was copied by hand, as doctor once suggested).
func TestRemoveAHandCopiedPackagedFile(t *testing.T) {
	m := newMachine(t)
	mustWrite(t, m.layout.Template.Target, templateV1)

	results := m.remove(Options{})

	expect(t, result(t, results, "theme template"), doctor.OK, "removed", "")
}

func TestManifestWriteFailure(t *testing.T) {
	m := newMachine(t)
	m.sys.FS = failingPath{OSFS{}, m.layout.Manifest}

	results := m.install(Options{})

	expect(t, result(t, results, "manifest"), doctor.Fail, "cannot write "+m.layout.Manifest, "")
}

// failingPath fails every write to one path.
type failingPath struct {
	OSFS
	path string
}

func (f failingPath) WriteFile(path string, data []byte, mode fs.FileMode) error {
	if path == f.path {
		return errors.New("read-only")
	}
	return f.OSFS.WriteFile(path, data, mode)
}

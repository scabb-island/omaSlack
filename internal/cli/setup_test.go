package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/setup"
)

// setupHome is an empty temporary home where systemctl reports an
// installed, disabled omaslack.service. ran collects every command run.
func setupHome(t *testing.T) (home string, sys func() setup.System, ran *[]string) {
	t.Helper()
	home = t.TempDir()
	ran = &[]string{}
	sys = func() setup.System {
		return setup.System{
			FS: setup.OSFS{},
			Exec: func(_ context.Context, name string, args ...string) (string, error) {
				*ran = append(*ran, strings.Join(append([]string{name}, args...), " "))
				return "LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n", nil
			},
			Getenv: func(k string) string { return map[string]string{"HOME": home}[k] },
		}
	}
	return home, sys, ran
}

func runSetupWith(sys func() setup.System, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(Env{Stdout: &out, Stderr: &errb, SetupSystem: sys}, append([]string{"setup"}, args...))
	return code, out.String(), errb.String()
}

// packagingDir is this checkout's packaging/ directory, relative to the
// test's working directory, as a developer would pass it to --from.
const packagingDir = "../../packaging"

func TestSetupInstallsFromACheckout(t *testing.T) {
	home, sys, _ := setupHome(t)

	code, out, errOut := runSetupWith(sys, "--from", packagingDir, "--window-rule")

	if code != ExitOK || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	template := filepath.Join(home, ".config/omarchy/themed/omaslack.toml.tpl")
	packaged, _ := filepath.Abs(filepath.Join(packagingDir, "omarchy/omaslack.toml.tpl"))
	if got, want := readFile(t, template), readFile(t, packaged); got != want {
		t.Errorf("installed template differs from %s", packaged)
	}
	wantIn(t, "stdout", out, "OK    theme template", "installed "+template,
		"OK    window rule", "WARN  window rule loaded", `require("hypr.omaslack")`,
		"OK    service", "enabled and started")
}

func TestSetupDryRunWritesNothing(t *testing.T) {
	home, sys, ran := setupHome(t)

	code, out, errOut := runSetupWith(sys, "--dry-run", "--from", packagingDir, "--window-rule")

	if code != ExitOK || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if entries, _ := os.ReadDir(home); len(entries) > 0 {
		t.Errorf("the dry run wrote to the home directory: %v", entries)
	}
	for _, c := range *ran {
		if !strings.HasPrefix(c, "systemctl --user show") {
			t.Errorf("the dry run ran %q", c)
		}
	}
	wantIn(t, "stdout", out, "would install", "would run systemctl --user enable --now omaslack")
}

func TestSetupRemove(t *testing.T) {
	home, sys, _ := setupHome(t)
	runSetupWith(sys, "--from", packagingDir)

	code, out, errOut := runSetupWith(sys, "--remove", "--from", packagingDir)

	if code != ExitOK || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".config/omarchy/themed/omaslack.toml.tpl")); err == nil {
		t.Error("the template is still there")
	}
	wantIn(t, "stdout", out, "removed", "omaslack auth remove", "rm -rf "+filepath.Join(home, ".local/share/omaslack"))
}

func TestSetupFailsWithoutThePackage(t *testing.T) {
	_, sys, _ := setupHome(t)

	code, out, errOut := runSetupWith(sys, "--from", t.TempDir())

	if code != ExitError || !strings.Contains(errOut, "some steps failed") {
		t.Fatalf("code=%d stderr=%q, want %d", code, errOut, ExitError)
	}
	wantIn(t, "stdout", out, "FAIL  theme template", "cannot read the packaged file")
}

func TestSetupUsage(t *testing.T) {
	_, sys, _ := setupHome(t)
	for _, args := range [][]string{{"--bogus"}, {"extra"}} {
		code, _, errOut := runSetupWith(sys, args...)
		if code != ExitUsage || !strings.Contains(errOut, "Usage: omaslack setup") {
			t.Errorf("%v: code=%d stderr=%q", args, code, errOut)
		}
	}
	code, out, _ := runSetupWith(sys, "--help")
	if code != ExitOK || !strings.Contains(out, "Usage: omaslack setup") {
		t.Errorf("--help: code=%d stdout=%q", code, out)
	}
}

func TestSetupWithoutAHome(t *testing.T) {
	sys := func() setup.System { return setup.System{FS: setup.OSFS{}, Getenv: func(string) string { return "" }} }
	code, _, errOut := runSetupWith(sys)
	if code != ExitError || !strings.Contains(errOut, "HOME is not set") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

package cli

import (
	"bytes"
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/doctor"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/version"
)

// doctorProbes is a machine where every check passes but the hook, which
// is missing, and the daemon, which fails when down is set.
func doctorProbes(t *testing.T, down bool) func() doctor.Probes {
	paths := doctor.PathsFor("/home/me", "/run/user/1000")
	store := keyring.NewFileStore(filepath.Join(t.TempDir(), "credentials"))
	return func() doctor.Probes {
		return doctor.Probes{
			Paths: paths,
			UID:   1000,
			Hello: func(context.Context) (doctor.Hello, error) {
				if down {
					return doctor.Hello{}, rpcclient.ErrUnavailable
				}
				return doctor.Hello{Protocol: version.Protocol, DaemonVersion: version.Version}, nil
			},
			Service: func(context.Context) (doctor.Unit, error) {
				return doctor.Unit{Load: "loaded", Active: "active", UnitFile: "enabled"}, nil
			},
			Stat: func(path string) (doctor.File, error) {
				switch path {
				case paths.Hook:
					return doctor.File{}, fs.ErrNotExist
				case paths.Socket:
					return doctor.File{Mode: fs.ModeSocket | 0o600, UID: 1000}, nil
				}
				return doctor.File{Mode: 0o600, UID: 1000}, nil
			},
			Keyring: func(context.Context) (keyring.ClosableStore, error) { return store, nil },
			Plugins: func(context.Context) ([]doctor.Plugin, error) {
				return []doctor.Plugin{{ID: doctor.PluginID, Enabled: true}}, nil
			},
			Shell: func(context.Context) (doctor.Shell, error) { return doctor.Shell{Running: true}, nil },
		}
	}
}

func runDoctorWith(probes func() doctor.Probes, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(Env{Stdout: &out, Stderr: &errb, DoctorProbes: probes}, append([]string{"doctor"}, args...))
	return code, out.String(), errb.String()
}

func TestDoctorWarningsDoNotFail(t *testing.T) {
	code, out, errOut := runDoctorWith(doctorProbes(t, false))
	if code != ExitOK || errOut != "" {
		t.Fatalf("code=%d stderr=%q, want 0 and no error", code, errOut)
	}
	// Spec omarchy-integration, "Missing hook": a warning with the
	// command that reinstalls it.
	wantIn(t, "stdout", out, "WARN  theme hook", "/home/me/.config/omarchy/hooks/theme-set.d/omaslack is missing",
		"fix: omaslack setup", "OK    daemon", "WARN  credentials")
}

func TestDoctorFailsWhenACheckFails(t *testing.T) {
	code, out, errOut := runDoctorWith(doctorProbes(t, true))
	if code != ExitError || !strings.Contains(errOut, "some checks failed") {
		t.Fatalf("code=%d stderr=%q, want %d", code, errOut, ExitError)
	}
	wantIn(t, "stdout", out, "FAIL  daemon", "fix: systemctl --user start omaslack")
}

func TestDoctorRejectsArguments(t *testing.T) {
	code, _, errOut := runDoctorWith(doctorProbes(t, false), "--fix")
	if code != ExitUsage || !strings.Contains(errOut, "Usage: omaslack doctor") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestDoctorReportsAWriteError(t *testing.T) {
	var errb bytes.Buffer
	code := Run(Env{Stdout: brokenWriter{}, Stderr: &errb, DoctorProbes: doctorProbes(t, false)}, []string{"doctor"})
	if code != ExitError || !strings.Contains(errb.String(), "print the report") {
		t.Errorf("code=%d stderr=%q", code, errb.String())
	}
}

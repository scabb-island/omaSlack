package cli

import (
	"bytes"
	"strings"
	"testing"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(Env{Stdout: &out, Stderr: &errb}, args)
	return code, out.String(), errb.String()
}

func TestNoArgsPrintsUsageAndFails(t *testing.T) {
	code, out, errOut := run()
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "Usage: omaslack") {
		t.Errorf("stderr has no usage: %q", errOut)
	}
}

func TestHelpGoesToStdout(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		code, out, _ := run(arg)
		if code != ExitOK || !strings.Contains(out, "Commands:") {
			t.Errorf("%s: code=%d stdout=%q", arg, code, out)
		}
	}
}

func TestUsageListsEverySubcommand(t *testing.T) {
	_, out, _ := run("help")
	for _, name := range []string{"auth", "ctl", "daemon", "doctor", "tui", "version"} {
		if !strings.Contains(out, "  "+name+" ") {
			t.Errorf("usage does not list %q:\n%s", name, out)
		}
	}
}

func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		code, out, _ := run(arg)
		if code != ExitOK {
			t.Fatalf("%s: exit code = %d", arg, code)
		}
		if !strings.HasPrefix(out, "omaslack ") || !strings.Contains(out, "protocol 1") {
			t.Errorf("%s: stdout = %q", arg, out)
		}
	}
}

func TestVersionRejectsArguments(t *testing.T) {
	code, _, errOut := run("version", "extra")
	if code != ExitError || !strings.Contains(errOut, "unexpected arguments") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errOut := run("frobnicate")
	if code != ExitUsage || !strings.Contains(errOut, `unknown command "frobnicate"`) {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestPlaceholdersSayWhichMilestone(t *testing.T) {
	cases := map[string]string{"daemon": "M1/M2", "tui": "M3", "ctl": "M2", "auth": "M1", "doctor": "M5"}
	for name, milestone := range cases {
		code, _, errOut := run(name)
		if code != ExitNotImplemented {
			t.Errorf("%s: exit code = %d, want %d", name, code, ExitNotImplemented)
		}
		if !strings.Contains(errOut, milestone) {
			t.Errorf("%s: stderr %q does not name %s", name, errOut, milestone)
		}
	}
}

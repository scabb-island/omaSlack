package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/tui/app"
)

func TestTUIRunsTheAppOnTheDaemon(t *testing.T) {
	d := startCtlDaemon(t)
	var out, errb bytes.Buffer
	env := d.env(strings.NewReader(""), &out, &errb)
	var got app.Options
	env.RunTUI = func(_ context.Context, o app.Options) error {
		got = o
		return nil
	}
	if code := Run(env, []string{"tui"}); code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, errb.String())
	}
	if got.Client == nil || got.Client.Hello() == nil || got.Themes == nil || got.Theme.Accent == nil {
		t.Fatalf("options %+v", got)
	}
}

func TestTUIReportsTheAppsError(t *testing.T) {
	d := startCtlDaemon(t)
	var errb bytes.Buffer
	env := d.env(strings.NewReader(""), &bytes.Buffer{}, &errb)
	env.RunTUI = func(context.Context, app.Options) error { return errors.New("terminal too small") }
	if code := Run(env, []string{"tui"}); code != ExitError || !strings.Contains(errb.String(), "terminal too small") {
		t.Fatalf("code=%d stderr=%q", code, errb.String())
	}
}

func TestTUIWithoutDaemon(t *testing.T) {
	var errb bytes.Buffer
	env := Env{Stdout: &bytes.Buffer{}, Stderr: &errb, DialDaemon: func(context.Context) (*rpcclient.Client, error) {
		return nil, rpcclient.ErrUnavailable
	}}
	if code := Run(env, []string{"tui"}); code != ExitUnavailable {
		t.Fatalf("code=%d stderr=%q", code, errb.String())
	}
}

func TestTUIUsage(t *testing.T) {
	code, out, _ := run("tui", "--help")
	if code != ExitOK || !strings.Contains(out, "org.omarchy.omaslack") || !strings.Contains(out, "omarchy-launch-or-focus-tui") {
		t.Fatalf("code=%d stdout=%q", code, out)
	}
	code, _, errOut := run("tui", "extra")
	if code != ExitUsage || !strings.Contains(errOut, "Usage: omaslack tui") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

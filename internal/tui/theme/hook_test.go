package theme

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// hookHelperEnv makes the test binary act as an omaslack process (see
// TestMain): it prints "ready", then "USR1" if it gets SIGUSR1 before its
// stdin ends, or "none".
const hookHelperEnv = "OMASLACK_HOOK_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(hookHelperEnv) != "" {
		hookHelper()
		return
	}
	os.Exit(m.Run())
}

func hookHelper() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1)
	fmt.Println("ready")
	eof := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); close(eof) }()
	select {
	case <-sigs:
		fmt.Println("USR1")
	case <-eof:
		select { // a signal sent just before stdin ended is still on its way
		case <-sigs:
			fmt.Println("USR1")
		case <-time.After(200 * time.Millisecond):
			fmt.Println("none")
		}
	}
}

// omaslackProcess is a helper started as `<test binary> <subcommand>`, so
// its /proc/<pid>/cmdline has the subcommand as argv[1], like omaslack's.
type omaslackProcess struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Scanner
}

func startOmaslack(t *testing.T, subcommand string) *omaslackProcess {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], subcommand) //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), hookHelperEnv+"=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &omaslackProcess{cmd: cmd, stdin: stdin, out: bufio.NewScanner(stdout)}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if line := p.line(); line != "ready" {
		t.Fatalf("%s helper said %q, want ready", subcommand, line)
	}
	return p
}

func (p *omaslackProcess) line() string {
	if p.out.Scan() {
		return p.out.Text()
	}
	return "(no output)"
}

// verdict ends the helper and says whether it got SIGUSR1.
func (p *omaslackProcess) verdict() string {
	_ = p.stdin.Close()
	return p.line()
}

// The hook signals the user's `omaslack tui` processes and leaves every
// other omaslack process alone: SIGUSR1 would terminate the daemon.
// pgrep is faked so only the helpers are candidates, never a real
// omaslack running on this machine.
func TestThemeSetHookSignalsOnlyTUIs(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("the hook needs bash")
	}
	if _, err := os.Stat("/proc/self/cmdline"); err != nil {
		t.Skip("the hook needs /proc")
	}
	hook, err := filepath.Abs("../../../packaging/omarchy/theme-set.d/omaslack")
	if err != nil {
		t.Fatal(err)
	}
	tui := startOmaslack(t, "tui")
	daemon := startOmaslack(t, "daemon")

	bin := t.TempDir()
	fakePgrep := fmt.Sprintf(`#!/bin/sh
[ "$*" = "-x -u %d omaslack" ] || { echo "unexpected pgrep $*" >&2; exit 2; }
echo %d
echo %d
echo 999999999
`, os.Getuid(), tui.cmd.Process.Pid, daemon.cmd.Process.Pid)
	if err := os.WriteFile(filepath.Join(bin, "pgrep"), []byte(fakePgrep), 0o755); err != nil { //nolint:gosec // an executable fake
		t.Fatal(err)
	}
	run := exec.CommandContext(t.Context(), bash, hook) //nolint:gosec // the hook under test
	run.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := run.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("hook: %v, output %q; want exit 0 and no output", err, out)
	}

	if got := tui.verdict(); got != "USR1" {
		t.Errorf("omaslack tui: %s, want USR1", got)
	}
	if got := daemon.verdict(); got != "none" {
		t.Errorf("omaslack daemon: %s, want no signal", got)
	}
}

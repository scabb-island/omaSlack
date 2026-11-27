// Package cli dispatches the omaslack subcommands.
//
// omaslack ships as a single binary (design.md, "Single binary"): the daemon,
// the TUI client, the control CLI and the auth/doctor tooling are subcommands,
// so client and daemon always speak the same protocol version.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/doctor"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/tui/app"
	"gitlab.com/scabb-island/omaslack/internal/version"
)

// Exit codes.
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitRPCError    = 4 // the daemon answered with a JSON-RPC error
	ExitUnavailable = 5 // the daemon could not be reached or went away
)

// Env is what a subcommand gets to work with. Injected so tests never touch
// the real process streams, keyring, Slack desktop app or network.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Stdin is what the user types or pastes. When it is a terminal,
	// secrets are read from it without echo.
	Stdin io.Reader
	// StdinIsTerminal says a user is at Stdin to answer prompts
	// (console.IsTerminal). Commands that prompt refuse to run without one.
	StdinIsTerminal bool
	// Keyring opens the credential store (keyring.Open).
	Keyring func(context.Context) (keyring.ClosableStore, error)
	// DesktopWorkspaces lists the workspaces of the Slack desktop app.
	DesktopWorkspaces func(context.Context) ([]auth.Workspace, error)
	// Browsers lists the Chromium browsers whose Slack web session can be
	// read as a desktop-session source. Nil means none are offered.
	Browsers func() []BrowserSession
	// Sources lists the desktop-session sources auth refresh re-reads (the
	// Slack desktop app and browser profiles). Nil means auth.LocalSources.
	Sources func() []auth.Source
	// SlackAPIURL is Slack's Web API base URL, ending in "/".
	SlackAPIURL string
	// DialDaemon connects to the daemon as omaslack ctl. Nil means
	// rpcclient.Dial on the real socket, starting the user service.
	DialDaemon func(context.Context) (*rpcclient.Client, error)
	// RunTUI runs the terminal client. Nil means app.Run, on the real
	// terminal.
	RunTUI func(context.Context, app.Options) error
	// DoctorProbes reads the system for omaslack doctor. Nil means
	// doctor.SystemProbes with Keyring.
	DoctorProbes func() doctor.Probes
}

// BrowserSession is one browser offered as a desktop-session source: its
// display name and a way to read the Slack web session it holds.
type BrowserSession struct {
	Name       string
	Workspaces func(context.Context) ([]auth.Workspace, error)
}

// usageError reports a command line that does not parse. Run prints it
// with the usage of the command and exits with ExitUsage.
type usageError struct {
	err   error
	usage string
}

func (e *usageError) Error() string { return e.err.Error() }

type command struct {
	summary string
	run     func(env Env, args []string) error
}

func commands() map[string]command {
	return map[string]command{
		"daemon":  {summary: "run the local daemon that talks to Slack", run: runDaemon},
		"tui":     {summary: "open the terminal client", run: runTUI},
		"ctl":     {summary: "call a daemon RPC method or stream its events", run: runCtl},
		"auth":    {summary: "add, list or remove Slack workspaces", run: runAuth},
		"doctor":  {summary: "check the installation and report problems", run: runDoctor},
		"version": {summary: "print version information", run: runVersion},
	}
}

// Run executes omaslack with args (without the program name) and returns the
// process exit code.
func Run(env Env, args []string) int {
	if len(args) == 0 {
		usage(env.Stderr)
		return ExitUsage
	}
	name := args[0]
	switch name {
	case "-h", "--help", "help":
		usage(env.Stdout)
		return ExitOK
	case "-v", "--version":
		name = "version"
	}
	cmd, ok := commands()[name]
	if !ok {
		fmt.Fprintf(env.Stderr, "omaslack: unknown command %q\n\n", name)
		usage(env.Stderr)
		return ExitUsage
	}
	return exitCode(env, name, cmd.run(env, args[1:]))
}

// exitCode reports err, if any, and returns the exit code it means.
func exitCode(env Env, name string, err error) int {
	if err == nil {
		return ExitOK
	}
	fmt.Fprintf(env.Stderr, "omaslack %s: %v\n", name, err)
	var usage *usageError
	if errors.As(err, &usage) {
		fmt.Fprint(env.Stderr, usage.usage)
		return ExitUsage
	}
	return daemonExitCode(err)
}

// daemonExitCode tells a daemon's error answer and an unreachable daemon
// apart from other errors, so scripts can react to each.
func daemonExitCode(err error) int {
	var rpcErr *rpc.Error
	switch {
	case errors.As(err, &rpcErr):
		return ExitRPCError
	case errors.Is(err, rpcclient.ErrUnavailable), errors.Is(err, rpcclient.ErrClosed):
		return ExitUnavailable
	}
	return ExitError
}

func runVersion(env Env, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))
	}
	_, err := fmt.Fprintln(env.Stdout, version.String())
	return err
}

func usage(w io.Writer) {
	cmds := commands()
	names := slices.Sorted(maps.Keys(cmds))

	var b strings.Builder
	b.WriteString("Usage: omaslack <command> [arguments]\n\nCommands:\n")
	for _, n := range names {
		fmt.Fprintf(&b, "  %-8s %s\n", n, cmds[n].summary)
	}
	_, _ = io.WriteString(w, b.String())
}

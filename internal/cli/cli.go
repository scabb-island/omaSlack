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

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/version"
)

// Exit codes.
const (
	ExitOK             = 0
	ExitError          = 1
	ExitUsage          = 2
	ExitNotImplemented = 3
	ExitRPCError       = 4 // the daemon answered with a JSON-RPC error
	ExitUnavailable    = 5 // the daemon could not be reached or went away
)

// ErrNotImplemented marks a subcommand whose milestone has not landed yet.
var ErrNotImplemented = errors.New("not implemented yet")

// Env is what a subcommand gets to work with. Injected so tests never touch
// the real process streams, keyring, Slack desktop app or network.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Stdin is what the user types or pastes. When it is a terminal,
	// secrets are read from it without echo.
	Stdin io.Reader
	// Keyring opens the credential store (keyring.Open).
	Keyring func(context.Context) (keyring.ClosableStore, error)
	// DesktopWorkspaces lists the workspaces of the Slack desktop app.
	DesktopWorkspaces func(context.Context) ([]auth.Workspace, error)
	// SlackAPIURL is Slack's Web API base URL, ending in "/".
	SlackAPIURL string
	// DialDaemon connects to the daemon as omaslack ctl. Nil means
	// rpcclient.Dial on the real socket, starting the user service.
	DialDaemon func(context.Context) (*rpcclient.Client, error)
}

// usageError reports a command line that does not parse. Run prints it
// with the usage of the command and exits with ExitUsage.
type usageError struct {
	err   error
	usage string
}

func (e *usageError) Error() string { return e.err.Error() }

type command struct {
	summary   string
	milestone string // non-empty while the subcommand is a placeholder
	run       func(env Env, args []string) error
}

func commands() map[string]command {
	return map[string]command{
		"daemon":  {summary: "run the local daemon that talks to Slack", run: runDaemon},
		"tui":     {summary: "open the terminal client", milestone: "M3"},
		"ctl":     {summary: "call a daemon RPC method or stream its events", run: runCtl},
		"auth":    {summary: "add, list or remove Slack workspaces", run: runAuth},
		"doctor":  {summary: "check the installation and report problems", milestone: "M5"},
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
	if cmd.run == nil {
		fmt.Fprintf(env.Stderr, "omaslack %s: %v (planned for %s)\n", name, ErrNotImplemented, cmd.milestone)
		return ExitNotImplemented
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
		c := cmds[n]
		line := fmt.Sprintf("  %-8s %s", n, c.summary)
		if c.milestone != "" {
			line += " (not implemented yet, " + c.milestone + ")"
		}
		b.WriteString(line + "\n")
	}
	_, _ = io.WriteString(w, b.String())
}

// Package cli dispatches the omaslack subcommands.
//
// omaslack ships as a single binary (design.md, "Single binary"): the daemon,
// the TUI client, the control CLI and the auth/doctor tooling are subcommands,
// so client and daemon always speak the same protocol version.
package cli

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/version"
)

// Exit codes.
const (
	ExitOK             = 0
	ExitError          = 1
	ExitUsage          = 2
	ExitNotImplemented = 3
)

// ErrNotImplemented marks a subcommand whose milestone has not landed yet.
var ErrNotImplemented = errors.New("not implemented yet")

// Env is what a subcommand gets to work with. Injected so tests never touch
// the real process streams.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
}

type command struct {
	summary   string
	milestone string // non-empty while the subcommand is a placeholder
	run       func(env Env, args []string) error
}

func commands() map[string]command {
	return map[string]command{
		"daemon":  {summary: "run the local daemon that talks to Slack", milestone: "M1/M2"},
		"tui":     {summary: "open the terminal client", milestone: "M3"},
		"ctl":     {summary: "call a daemon RPC method or stream its events", milestone: "M2"},
		"auth":    {summary: "add, list or remove Slack workspaces", milestone: "M1"},
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
	if err := cmd.run(env, args[1:]); err != nil {
		fmt.Fprintf(env.Stderr, "omaslack %s: %v\n", name, err)
		return ExitError
	}
	return ExitOK
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

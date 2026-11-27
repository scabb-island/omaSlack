package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
)

// probeTimeout bounds each probe that talks to another process.
const probeTimeout = 5 * time.Second

// errNotStarting stops rpcclient.Dial from starting the daemon: doctor
// reports what it finds and changes nothing.
var errNotStarting = errors.New("doctor does not start the daemon")

// Output runs a command and returns its standard output.
type Output func(ctx context.Context, name string, args ...string) (string, error)

// SystemProbes reads this machine. openKeyring is keyring.Open in
// production, as cli.Env.Keyring.
func SystemProbes(openKeyring func(context.Context) (keyring.ClosableStore, error)) Probes {
	home, _ := os.UserHomeDir()
	paths := PathsFor(home, os.Getenv("XDG_RUNTIME_DIR"))
	paths.CredentialsFile, _ = keyring.DefaultFilePath()
	return Probes{
		Paths:          paths,
		UID:            os.Getuid(),
		Hello:          func(ctx context.Context) (Hello, error) { return dialHello(ctx, paths.Socket) },
		Service:        func(ctx context.Context) (Unit, error) { return service(ctx, output) },
		Stat:           stat,
		Keyring:        openKeyring,
		SessionSources: sessionSources,
		Plugins:        func(ctx context.Context) ([]Plugin, error) { return plugins(ctx, output) },
		Shell:          func(ctx context.Context) (Shell, error) { return shell(ctx, exec.LookPath, output) },
	}
}

// sessionSources names the desktop-session sources signed in to Slack on
// this machine, reading only plaintext tokens, so it never prompts for the
// keyring (SignedIn).
func sessionSources() []string {
	var names []string
	if dir, err := auth.FindDesktopDir(); err == nil && (auth.Desktop{Dir: dir}).SignedIn() {
		names = append(names, "the Slack desktop app")
	}
	for _, b := range auth.FindBrowsers() {
		if b.SignedIn() {
			names = append(names, b.Name)
		}
	}
	return names
}

func output(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // fixed commands: systemctl, omarchy, omarchy-shell, omarchy-version
	return string(out), err
}

// dialHello says hello to the daemon on socket without starting it.
func dialHello(ctx context.Context, socket string) (Hello, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	c, err := rpcclient.Dial(ctx, rpcclient.Options{
		Client: "omaslack-doctor",
		Path:   socket,
		Run:    func(context.Context, string, ...string) error { return errNotStarting },
	})
	if err != nil {
		return Hello{}, err
	}
	defer c.Close()
	return parseHello(c.Hello())
}

// parseHello reads the fields doctor uses from the hello result
// (rpc.helloResult plus the daemon's workspaces).
func parseHello(raw []byte) (Hello, error) {
	var h Hello
	if err := json.Unmarshal(raw, &h); err != nil {
		return Hello{}, fmt.Errorf("unexpected hello answer: %w", err)
	}
	return h, nil
}

func service(ctx context.Context, run Output) (Unit, error) {
	out, err := run(ctx, "systemctl", "--user", "show", "omaslack", "--property=LoadState,ActiveState,UnitFileState")
	if err != nil {
		return Unit{}, err
	}
	return parseUnit(out), nil
}

// parseUnit reads systemctl show's KEY=value lines.
func parseUnit(out string) Unit {
	var u Unit
	fields := map[string]*string{"LoadState": &u.Load, "ActiveState": &u.Active, "UnitFileState": &u.UnitFile}
	for _, line := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(line, "=")
		if field, ok := fields[k]; ok {
			*field = v
		}
	}
	return u
}

func plugins(ctx context.Context, run Output) ([]Plugin, error) {
	out, err := run(ctx, "omarchy", "plugin", "list", "--json")
	if err != nil {
		return nil, err
	}
	var list []Plugin
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("unexpected omarchy plugin list output: %w", err)
	}
	return list, nil
}

// shell finds omarchy-shell, pings it (the ping fails, without starting
// it, when the shell is not running) and reads Omarchy's version.
func shell(ctx context.Context, lookPath func(string) (string, error), run Output) (Shell, error) {
	if _, err := lookPath("omarchy-shell"); err != nil {
		return Shell{}, err
	}
	_, err := run(ctx, "omarchy-shell", "shell", "ping")
	version, _ := run(ctx, "omarchy-version")
	return Shell{Running: err == nil, Version: strings.TrimSpace(version)}, nil
}

func stat(path string) (File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return File{}, err
	}
	// omaslack runs on Linux only, where Sys is always a *syscall.Stat_t.
	return File{Mode: info.Mode(), UID: int(info.Sys().(*syscall.Stat_t).Uid)}, nil
}

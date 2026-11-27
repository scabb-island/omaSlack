package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"

	"gitlab.com/scabb-island/omaslack/internal/daemon"
	"gitlab.com/scabb-island/omaslack/internal/notify"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
)

// daemonUsage is the usage of `omaslack daemon`.
const daemonUsage = `Usage: omaslack daemon

Runs the daemon in the foreground: one sync engine per workspace in the
keyring, served on $XDG_RUNTIME_DIR/omaslack.sock. It logs to stderr.
Normally the omaslack systemd user service runs it.

Signals:
  SIGINT, SIGTERM  shut down in order
  SIGHUP           read the keyring again, so omaslack auth add|remove takes
                   effect (systemctl --user reload omaslack), and the
                   [notifications] settings of
                   $XDG_CONFIG_HOME/omaslack/config.toml
`

// runDaemon runs the daemon until SIGINT or SIGTERM.
func runDaemon(env Env, args []string) error {
	if len(args) > 0 {
		return &usageError{err: fmt.Errorf("unexpected arguments: %s", strings.Join(args, " ")), usage: daemonUsage}
	}
	paths, err := daemon.DefaultPaths(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	defer signal.Stop(reload)

	log := slog.New(slog.NewTextHandler(env.Stderr, nil))
	err = daemon.Run(ctx, daemon.Config{
		Paths:       paths,
		Keyring:     env.Keyring,
		NewProvider: daemon.SlackProviders(env.SlackAPIURL, log),
		Log:         log,
		Reload:      reload,
		Notifications: func(events notify.Events) notify.Sender {
			return notify.NewBus(func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() }, events, log)
		},
		LaunchTUI: daemon.LaunchTUI,
	})
	if errors.Is(err, rpc.ErrSocketInUse) {
		return fmt.Errorf("another omaslack daemon is already running on %s", paths.Socket)
	}
	return err
}

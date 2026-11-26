// Package dbustest runs a private D-Bus daemon for tests, so code that talks
// to the session bus is tested without touching the user's real session,
// keyring or wallet.
//
// Adapted from github.com/gammons/slk internal/slackdesktop/password_linux_test.go (MIT, Copyright (c) 2026 Grant Ammons).
package dbustest

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/godbus/dbus/v5"
)

// The config has no service directories, so calling a name nobody owns
// fails instead of D-Bus-activating the machine's real keyring daemon onto
// this bus. unix:tmpdir keeps the socket path short: t.TempDir() names can
// pass the sun_path length limit.
const busConfig = `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:tmpdir=/tmp</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*"/>
    <allow receive_sender="*"/>
    <allow own="*"/>
  </policy>
</busconfig>
`

// StartBus runs a private dbus-daemon for one test and returns its address.
// It skips the test when dbus-daemon is not installed (CI's image has none).
func StartBus(t *testing.T) string {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	addr, err := startDaemon(t, daemon)
	if err != nil {
		t.Fatalf("start dbus-daemon: %v", err)
	}
	return addr
}

// startDaemon runs daemon with the private bus config until the test ends
// and returns the address it prints once its socket is bound.
func startDaemon(t *testing.T, daemon string) (string, error) {
	config := filepath.Join(t.TempDir(), "bus.conf")
	if err := os.WriteFile(config, []byte(busConfig), 0o600); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(t.Context(), daemon, "--config-file="+config, "--nofork", "--nopidfile", "--print-address") //nolint:gosec // the daemon comes from PATH, as in any test
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	pipe, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		return "", err
	}
	// t.Context is cancelled just before cleanups run, which sends SIGTERM
	// (not a kill: the daemon unlinks its socket only on a clean exit); this
	// cleanup reaps it.
	t.Cleanup(func() { _ = cmd.Wait() })
	addr, err := bufio.NewReader(pipe).ReadString('\n')
	return strings.TrimSpace(addr), err
}

// Connect opens a private connection to the bus at addr, closed when the
// test ends.
func Connect(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatalf("connect to test bus: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// Serve exports each object on a new connection to addr, takes name and
// returns the connection, so more objects can be exported on it later.
func Serve(t *testing.T, addr, name string, objects ...Object) *dbus.Conn {
	t.Helper()
	conn := Connect(t, addr)
	if err := serve(conn, name, objects...); err != nil {
		t.Fatal(err)
	}
	return conn
}

func serve(conn *dbus.Conn, name string, objects ...Object) error {
	for _, o := range objects {
		if err := conn.Export(o.Value, o.Path, o.Interface); err != nil {
			return fmt.Errorf("export %s: %w", o.Interface, err)
		}
	}
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err == nil && reply != dbus.RequestNameReplyPrimaryOwner {
		err = fmt.Errorf("reply %v, want primary owner", reply)
	}
	if err != nil {
		return fmt.Errorf("request name %s: %w", name, err)
	}
	return nil
}

// Object is one exported D-Bus object: the methods of Value, served at Path
// under Interface.
type Object struct {
	Value     any
	Path      dbus.ObjectPath
	Interface string
}

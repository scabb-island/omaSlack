package rpc

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func socketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "omaslack.sock")
}

func listen(t *testing.T, path string) *Listener {
	t.Helper()
	l, err := Listen(context.Background(), path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return l
}

func dial(t *testing.T, path string) *net.UnixConn {
	t.Helper()
	var d net.Dialer
	c, err := d.DialContext(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	return c.(*net.UnixConn)
}

func TestListenCreatesAPrivateSocket(t *testing.T) {
	path := socketPath(t)
	l := listen(t, path)
	defer func() { _ = l.Close() }()

	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Type() != fs.ModeSocket || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want a socket with 0600", fi.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("the directory holds %d entries, want only the socket", len(entries))
	}
	dial(t, path)
}

func TestListenReplacesAStaleSocket(t *testing.T) {
	path := socketPath(t)
	stale := listen(t, path)
	_ = stale.UnixListener.Close() // a crashed daemon: the file stays, nobody listens

	l := listen(t, path)
	defer func() { _ = l.Close() }()
	dial(t, path)
}

func TestListenRefusesASocketInUse(t *testing.T) {
	path := socketPath(t)
	live := listen(t, path)
	defer func() { _ = live.Close() }()

	if _, err := Listen(context.Background(), path); !errors.Is(err, ErrSocketInUse) {
		t.Fatalf("Listen = %v, want ErrSocketInUse", err)
	}
	dial(t, path)
}

func TestListenNeverReplacesAFileThatIsNotASocket(t *testing.T) {
	path := socketPath(t)
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(context.Background(), path); !errors.Is(err, ErrNotSocket) {
		t.Fatalf("Listen = %v, want ErrNotSocket", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "keep me" {
		t.Fatalf("the file was touched: %q", b)
	}
}

func TestListenFailsInAMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "omaslack.sock")
	if _, err := Listen(context.Background(), path); err == nil {
		t.Fatal("Listen succeeded in a missing directory")
	}
}

func TestListenFailsWhenTheSocketCannotBeChecked(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads any directory (the CI container runs as root)")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if _, err := Listen(context.Background(), filepath.Join(dir, "omaslack.sock")); err == nil {
		t.Fatal("Listen succeeded in an unreadable directory")
	}
}

func TestCloseRemovesTheSocket(t *testing.T) {
	path := socketPath(t)
	l := listen(t, path)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("socket still there: %v", err)
	}
}

func TestCloseLeavesASocketThatReplacedOurs(t *testing.T) {
	path := socketPath(t)
	old := listen(t, path)
	other := filepath.Join(filepath.Dir(path), "other.sock")
	newer := listen(t, other)
	defer func() { _ = newer.Close() }()
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}

	_ = old.Close()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("Close removed a socket it did not own: %v", err)
	}
}

// Spec daemon-api, "Another user tries to connect": the decision itself.
func TestOnlyTheOwnerIsAllowed(t *testing.T) {
	if !sameUser(1000, 1000) {
		t.Error("the owner was rejected")
	}
	if sameUser(1001, 1000) {
		t.Error("another user was allowed")
	}
}

func TestListenFailsWhenThePathIsTooLongForASocket(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(context.Background(), filepath.Join(dir, "omaslack.sock")); err == nil {
		t.Fatal("Listen succeeded beyond the unix socket path limit")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("left %d entries behind", len(entries))
	}
}

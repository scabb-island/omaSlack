package rpc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
)

var (
	// ErrSocketInUse means another daemon already answers on the path.
	ErrSocketInUse = errors.New("rpc: socket already in use")
	// ErrNotSocket means the path holds something other than a socket,
	// which Listen never replaces.
	ErrNotSocket = errors.New("rpc: path exists and is not a socket")
)

// Listener is the daemon's private socket (spec daemon-api, "Private local
// socket"). Close stops accepting and removes the socket file.
type Listener struct {
	*net.UnixListener
	path string
	file os.FileInfo // the socket file we created, to never remove another
}

// Listen opens a unix socket at path that only its owner can use. The
// socket is bound inside a fresh 0700 directory, set to 0600 and only then
// renamed onto path, so it is never reachable with looser permissions. A
// stale socket left by a crashed daemon is replaced by that rename; a live
// one, or a file that is not a socket, makes Listen fail.
func Listen(ctx context.Context, path string) (*Listener, error) {
	if err := checkFree(ctx, path); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(filepath.Dir(path), ".omaslack-")
	if err != nil {
		return nil, fmt.Errorf("rpc: listen: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	tmp := filepath.Join(dir, "s")
	var lc net.ListenConfig
	nl, err := lc.Listen(ctx, "unix", tmp)
	if err != nil {
		return nil, fmt.Errorf("rpc: listen: %w", err)
	}
	// The listener only unlinks tmp on close, which the rename moved away.
	ul := nl.(*net.UnixListener)
	l := &Listener{UnixListener: ul, path: path}
	if err := l.publish(tmp); err != nil {
		_ = ul.Close()
		return nil, fmt.Errorf("rpc: listen: %w", err)
	}
	return l, nil
}

// publish makes the socket private, moves it into place and remembers
// which file it is. Each step fails only on an I/O error the caller can do
// nothing about, so they share one exit.
func (l *Listener) publish(tmp string) error {
	err := os.Chmod(tmp, 0o600)
	if err == nil {
		err = os.Rename(tmp, l.path)
	}
	if err == nil {
		l.file, err = os.Lstat(l.path)
	}
	return err
}

// checkFree fails unless path is missing or a socket nobody answers on.
func checkFree(ctx context.Context, path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rpc: listen: %w", err)
	}
	if fi.Mode().Type() != fs.ModeSocket {
		return fmt.Errorf("%w: %s", ErrNotSocket, path)
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil // stale
	}
	_ = c.Close()
	return fmt.Errorf("%w: %s", ErrSocketInUse, path)
}

// Close stops accepting and removes the socket file, unless another
// daemon has replaced it since.
func (l *Listener) Close() error {
	err := l.UnixListener.Close()
	if fi, statErr := os.Lstat(l.path); statErr == nil && os.SameFile(fi, l.file) {
		err = errors.Join(err, os.Remove(l.path))
	}
	return err
}

// Adapted from github.com/gammons/slk cmd/slk/onboarding_browser.go and cmd/slk/flushinput_linux.go (MIT, Copyright (c) 2026 Grant Ammons).

package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// terminal is a Console input that is a terminal.
type terminal struct {
	in io.Reader
	fd int
}

// terminalOf returns in as a terminal, when it is one.
func terminalOf(in io.Reader) (terminal, bool) {
	f, ok := in.(*os.File)
	if !ok {
		return terminal{}, false
	}
	fd := int(f.Fd()) //nolint:gosec // a file descriptor fits in an int
	return terminal{in: in, fd: fd}, term.IsTerminal(fd)
}

// readSecret reads a line with echo off. The Enter that ends it is not
// echoed either, so the line break goes to out.
func (t terminal) readSecret(out io.Writer) (string, error) {
	b, err := term.ReadPassword(t.fd)
	fmt.Fprintln(out)
	return strings.TrimSpace(string(b)), err
}

// readPaste reads a paste in raw mode, which neither echoes it nor limits
// its line length, then discards what is left of it.
func (t terminal) readPaste(out io.Writer) (string, error) {
	old, err := term.MakeRaw(t.fd)
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(t.fd, old) }()
	s, err := ReadPaste(t.in)
	drainInput(t.pending, t.flush, time.Sleep)
	fmt.Fprint(out, "\r\n")
	return s, err
}

// pending returns how many bytes of input wait to be read.
func (t terminal) pending() (int, error) { return unix.IoctlGetInt(t.fd, unix.TIOCINQ) }

// flush discards input the terminal has received but nobody has read.
func (t terminal) flush() error { return unix.IoctlSetInt(t.fd, unix.TCFLSH, unix.TCIFLUSH) }

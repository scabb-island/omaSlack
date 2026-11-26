package console

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal: the test types into master and the
// Console reads from tty, as from a real terminal. It skips where no pty
// can be opened.
func openPTY(t *testing.T) (master, tty *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("unlock pty: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Skipf("pty number: %v", err)
	}
	tty, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open pty: %v", err)
	}
	t.Cleanup(func() { _ = tty.Close() })
	return master, tty
}

// waitForMode waits until the terminal's local flags have none of off.
func waitForMode(t *testing.T, tty *os.File, off uint32) {
	t.Helper()
	for range 200 {
		tios, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS)
		if err != nil {
			t.Fatal(err)
		}
		if tios.Lflag&off == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the terminal never left its default mode")
}

func localFlags(t *testing.T, tty *os.File) uint32 {
	t.Helper()
	tios, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	return tios.Lflag
}

type answer struct {
	s   string
	err error
}

// typeInto runs ask in the background, types input once the terminal is
// out of mode off, and returns the answer and what the terminal echoed.
func typeInto(t *testing.T, master, tty *os.File, off uint32, input string, ask func() (string, error)) (answer, string) {
	t.Helper()
	done := make(chan answer, 1)
	go func() {
		s, err := ask()
		done <- answer{s, err}
	}()
	waitForMode(t, tty, off)
	if _, err := master.WriteString(input); err != nil {
		t.Fatal(err)
	}
	got := <-done
	// Whatever the terminal echoed reaches master before this marker.
	if _, err := tty.WriteString("@end@"); err != nil {
		t.Fatal(err)
	}
	var echo []byte
	buf := make([]byte, 4096)
	for !bytes.Contains(echo, []byte("@end@")) {
		n, err := master.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		echo = append(echo, buf[:n]...)
	}
	return got, string(echo)
}

func TestAskSecretOnATerminalDoesNotEcho(t *testing.T) {
	master, tty := openPTY(t)
	before := localFlags(t, tty)
	var out bytes.Buffer
	c := Console{In: tty, Out: &out}
	got, echo := typeInto(t, master, tty, unix.ECHO, "xoxp-secret\n", func() (string, error) { return c.AskSecret("Token: ") })
	if got.s != "xoxp-secret" || got.err != nil {
		t.Errorf("AskSecret = %q, %v", got.s, got.err)
	}
	if bytes.Contains([]byte(echo), []byte("secret")) {
		t.Errorf("the terminal echoed the secret: %q", echo)
	}
	if out.String() != "Token: \n" {
		t.Errorf("out = %q, want the prompt and a line break", out.String())
	}
	if after := localFlags(t, tty); after != before {
		t.Errorf("terminal flags %#x after, %#x before", after, before)
	}
}

func TestAskPasteOnATerminalIsRawAndDrained(t *testing.T) {
	master, tty := openPTY(t)
	before := localFlags(t, tty)
	var out bytes.Buffer
	c := Console{In: tty, Out: &out}
	got, echo := typeInto(t, master, tty, unix.ECHO|unix.ICANON, "xoxc-1-2\rxoxd-left-over", func() (string, error) { return c.AskPaste("cURL: ") })
	if got.s != "xoxc-1-2\n" || got.err != nil {
		t.Errorf("AskPaste = %q, %v", got.s, got.err)
	}
	if bytes.Contains([]byte(echo), []byte("xox")) {
		t.Errorf("the terminal echoed the paste: %q", echo)
	}
	if n, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCINQ); err != nil || n != 0 {
		t.Errorf("%d bytes left for the shell (%v), want none", n, err)
	}
	if out.String() != "cURL: \r\n" {
		t.Errorf("out = %q", out.String())
	}
	if after := localFlags(t, tty); after != before {
		t.Errorf("terminal flags %#x after, %#x before", after, before)
	}
}

func TestIsTerminal(t *testing.T) {
	if IsTerminal(strings.NewReader("yes\n")) {
		t.Error("a strings.Reader is a terminal")
	}
	file, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if IsTerminal(file) {
		t.Error("a regular file is a terminal")
	}
	_, tty := openPTY(t)
	if !IsTerminal(tty) {
		t.Error("a pseudo-terminal is not a terminal")
	}
}

// Adapted from github.com/gammons/slk cmd/slk/onboarding_browser.go (MIT, Copyright (c) 2026 Grant Ammons).

// Package console reads what the user types or pastes at a terminal:
// answers, secrets without echo, and multi-line pastes such as a
// browser's "Copy as cURL".
package console

import (
	"errors"
	"io"
	"strings"
	"time"
)

// ErrCancelled reports a paste the user cancelled with Ctrl-C.
var ErrCancelled = errors.New("cancelled")

// pasteComplete reports whether the text read so far ends a pasted command.
// A line ending with a continuation (\ for bash, ^ for cmd, ` for
// PowerShell) asks for more. The PowerShell form needs two more rules: it
// opens with "$session = ..." lines that end with no continuation at all,
// so it is only complete once its Invoke-WebRequest command is; and its
// -Headers @{ ... } block spans lines that end with none either, so it is
// not complete while a @{ is still open.
func pasteComplete(s string) bool {
	s = strings.TrimRight(s, "\n")
	if strings.TrimSpace(s) == "" {
		return false
	}
	last := strings.TrimRight(s[strings.LastIndexByte(s, '\n')+1:], " \t")
	if strings.HasSuffix(last, `\`) || strings.HasSuffix(last, "^") || strings.HasSuffix(last, "`") {
		return false
	}
	return powershellComplete(s)
}

// powershellComplete applies the PowerShell rules of pasteComplete to a
// paste whose last line has no continuation.
func powershellComplete(s string) bool {
	if !strings.Contains(s, "Invoke-WebRequest") {
		return !strings.HasPrefix(strings.TrimSpace(s), "$session")
	}
	closed := 0
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "}") {
			closed++
		}
	}
	return strings.Count(s, "@{") <= closed
}

// ReadPaste reads a paste byte by byte until it is complete (see
// pasteComplete), until Ctrl-D or until the input ends. Ctrl-C cancels
// with ErrCancelled. On a terminal it expects raw mode: in canonical mode
// Linux drops anything past 4095 bytes on a line, and a copied cURL line
// with its cookies easily goes past that. It reads one byte at a time, so
// from a pipe it consumes nothing past the paste.
func ReadPaste(r io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if done, cerr := takePaste(&b, buf[:n]); done {
			return b.String(), cerr
		}
		if errors.Is(err, io.EOF) {
			return b.String(), nil
		}
		if err != nil {
			return "", err
		}
	}
}

// takePaste appends chunk to b, stopping at a control key or at the end
// of a complete paste.
func takePaste(b *strings.Builder, chunk []byte) (done bool, err error) {
	for _, c := range chunk {
		switch c {
		case 3: // Ctrl-C
			b.Reset()
			return true, ErrCancelled
		case 4: // Ctrl-D
			return true, nil
		case '\r':
			c = '\n'
		}
		b.WriteByte(c)
		if c == '\n' && pasteComplete(b.String()) {
			return true, nil
		}
	}
	return false, nil
}

// Whatever is still queued once ReadPaste stops (the tail of a paste it
// did not recognise, keys typed meanwhile) holds secrets: it must not be
// left for the shell to read once omaslack exits. A paste reaches the
// terminal in chunks, slower over SSH, so a single flush right away can
// miss the tail. Input is discarded until it has been quiet for
// drainQuietPolls polls in a row (500 ms), for drainMaxPolls polls (5 s)
// at most.
const (
	drainPoll       = 50 * time.Millisecond
	drainQuietPolls = 10
	drainMaxPolls   = 100
)

// drainInput discards pending input until none has arrived for
// drainQuietPolls polls, or drainMaxPolls have passed. pending, flush and
// sleep are injected for tests.
func drainInput(pending func() (int, error), flush func() error, sleep func(time.Duration)) {
	quiet := 0
	for polls := 0; polls < drainMaxPolls && quiet < drainQuietPolls; polls++ {
		if n, err := pending(); err == nil && n > 0 {
			_ = flush()
			quiet = 0
		} else {
			quiet++
		}
		sleep(drainPoll)
	}
	_ = flush()
}

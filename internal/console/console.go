package console

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Console is the user's end of a command: In is what they type or paste,
// Out is where the prompts go. When In is a terminal, secrets and pastes
// are read without echo; otherwise (a pipe, a test) they are read as
// plain input.
type Console struct {
	In  io.Reader
	Out io.Writer
}

// Ask prints prompt and returns the next line, trimmed. At the end of the
// input the answer is empty.
func (c Console) Ask(prompt string) (string, error) {
	fmt.Fprint(c.Out, prompt)
	return readLine(c.In)
}

// AskSecret prints prompt and returns the next line, trimmed, without
// echoing it on a terminal.
func (c Console) AskSecret(prompt string) (string, error) {
	fmt.Fprint(c.Out, prompt)
	if t, ok := terminalOf(c.In); ok {
		return t.readSecret(c.Out)
	}
	return readLine(c.In)
}

// AskPaste prints prompt and returns a paste read with ReadPaste. On a
// terminal it is read in raw mode, without echo, and whatever follows it
// is discarded so no secret is left for the shell.
func (c Console) AskPaste(prompt string) (string, error) {
	fmt.Fprint(c.Out, prompt)
	if t, ok := terminalOf(c.In); ok {
		return t.readPaste(c.Out)
	}
	return ReadPaste(c.In)
}

// readLine reads up to the next newline one byte at a time, so nothing
// past it is consumed, and returns the line trimmed.
func readLine(r io.Reader) (string, error) {
	var b strings.Builder
	c := make([]byte, 1)
	for {
		n, err := r.Read(c)
		if n == 1 && c[0] == '\n' {
			break
		}
		b.Write(c[:n])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(b.String()), nil
}

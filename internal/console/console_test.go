package console

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newConsole(in string) (Console, *bytes.Buffer) {
	var out bytes.Buffer
	return Console{In: strings.NewReader(in), Out: &out}, &out
}

func TestAskReadsOneTrimmedLinePerPrompt(t *testing.T) {
	c, out := newConsole("  yes \r\n2\nrest")
	first, err1 := c.Ask("Continue? ")
	second, err2 := c.Ask("Pick: ")
	third, err3 := c.Ask("More: ")
	if first != "yes" || second != "2" || third != "rest" || errors.Join(err1, err2, err3) != nil {
		t.Errorf("answers = %q, %q, %q (%v, %v, %v)", first, second, third, err1, err2, err3)
	}
	if out.String() != "Continue? Pick: More: " {
		t.Errorf("prompts = %q", out.String())
	}
}

func TestAskAtTheEndOfInputIsEmpty(t *testing.T) {
	c, _ := newConsole("")
	if got, err := c.Ask("? "); got != "" || err != nil {
		t.Errorf("Ask = %q, %v; want an empty answer", got, err)
	}
}

func TestAskReportsReadErrors(t *testing.T) {
	c := Console{In: failingReader{}, Out: &bytes.Buffer{}}
	if _, err := c.Ask("? "); !errors.Is(err, errBroken) {
		t.Errorf("err = %v, want the read error", err)
	}
	if _, err := c.AskSecret("? "); !errors.Is(err, errBroken) {
		t.Errorf("AskSecret err = %v, want the read error", err)
	}
}

// Without a terminal (a pipe, a test) a secret is just the next line.
func TestAskSecretWithoutATerminalReadsALine(t *testing.T) {
	c, out := newConsole("xoxp-1\nxapp-2\n")
	first, _ := c.AskSecret("User token: ")
	second, err := c.AskSecret("App token: ")
	if first != "xoxp-1" || second != "xapp-2" || err != nil {
		t.Errorf("secrets = %q, %q, %v", first, second, err)
	}
	if out.String() != "User token: App token: " {
		t.Errorf("prompts = %q", out.String())
	}
}

// Without a terminal a paste ends where a pasted command would, so a
// piped token and cookie can be read one prompt each.
func TestAskPasteWithoutATerminal(t *testing.T) {
	c, out := newConsole(bashCurl + "xoxd-Q\n")
	first, _ := c.AskPaste("cURL: ")
	second, err := c.AskPaste("Cookie: ")
	if first != bashCurl || second != "xoxd-Q\n" || err != nil {
		t.Errorf("pastes = %q, %q, %v", first, second, err)
	}
	if out.String() != "cURL: Cookie: " {
		t.Errorf("prompts = %q", out.String())
	}
}

package render

import (
	"iter"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// wrap breaks at spaces and, for a word longer than the line, inside the
// word. Widths count terminal cells, so CJK and emoji take two. Every line
// comes out self-contained (see restyle).
func wrap(s string, width int) []string {
	if width > 0 {
		s = ansi.Hardwrap(ansi.Wordwrap(s, width, ""), width, true)
	}
	return restyle(strings.Split(s, "\n"))
}

// restyle makes each line open the styles and hyperlink still active from
// the line before, and close what it leaves open. x/ansi wraps a styled
// run without doing so, and the app places lines independently.
func restyle(lines []string) []string {
	var pen penState
	for i, l := range lines {
		open := pen.open()
		for seq := range escapes(l) {
			pen.apply(seq)
		}
		lines[i] = open + l + pen.close()
	}
	return lines
}

// escapes yields the SGR and OSC 8 sequences in s. Messages are cleaned of
// control characters first, so every ESC in s is one render wrote.
func escapes(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for i := strings.IndexByte(s, '\x1b'); i >= 0; i = strings.IndexByte(s, '\x1b') {
			s = s[i:]
			n := seqLen(s)
			seq := s[:n]
			s = s[n:]
			if (strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") || strings.HasPrefix(seq, "\x1b]8;")) && !yield(seq) {
				return
			}
		}
	}
}

// seqLen is the length of the CSI or OSC sequence s starts with (1 when
// it is cut short). style ends its OSC sequences with BEL.
func seqLen(s string) int {
	j := -1
	switch {
	case strings.HasPrefix(s, "\x1b["):
		j = strings.IndexFunc(s[2:], func(r rune) bool { return r >= 0x40 && r <= 0x7e }) + 2
	case strings.HasPrefix(s, "\x1b]"):
		j = strings.IndexByte(s, '\x07')
	}
	return max(j+1, 1)
}

const (
	sgrReset  = "\x1b[m"
	linkClose = "\x1b]8;;\x07"
)

// penState is the SGR and hyperlink in effect at a point of the text.
type penState struct{ sgr, link string }

func (p *penState) apply(seq string) {
	if params, ok := strings.CutPrefix(seq, "\x1b]8;"); ok {
		_, uri, _ := strings.Cut(params, ";")
		p.link = seq
		if strings.TrimRight(uri, "\x07\x1b\\") == "" {
			p.link = ""
		}
		return
	}
	if seq == sgrReset || seq == "\x1b[0m" {
		p.sgr = ""
		return
	}
	p.sgr += seq
}

func (p penState) open() string { return p.link + p.sgr }

func (p penState) close() string {
	var s string
	if p.sgr != "" {
		s += sgrReset
	}
	if p.link != "" {
		s += linkClose
	}
	return s
}

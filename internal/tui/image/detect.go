package image

import (
	"slices"
	"strings"
)

// Detect picks the best protocol from the environment (TERM, TERM_PROGRAM,
// KITTY_WINDOW_ID, …), never by querying the terminal, so it is cheap and
// testable (task 3.5). OMASLACK_IMAGES=kitty|sixel|halfblock forces a
// protocol for terminals the rules get wrong; other values are ignored.
// Otherwise the first matching rule wins, and HalfBlock (Alacritty and any
// unknown terminal) is the fallback.
func Detect(getenv func(string) string) Protocol {
	if p, ok := protocolNames[getenv("OMASLACK_IMAGES")]; ok {
		return p
	}
	for _, r := range detectRules {
		if r.match(getenv(r.env)) {
			return r.protocol
		}
	}
	return HalfBlock
}

type detectRule struct {
	env      string
	match    func(string) bool
	protocol Protocol
}

// detectRules, in order. Multiplexers come first: inside tmux, zellij or
// screen, graphics need a passthrough the TUI does not do, even when the
// outer terminal (say kitty, whose KITTY_WINDOW_ID leaks in) has them.
var detectRules = []detectRule{
	{"TMUX", nonEmpty, HalfBlock},
	{"ZELLIJ", nonEmpty, HalfBlock},
	{"TERM", prefixOf("screen", "tmux"), HalfBlock},

	{"KITTY_WINDOW_ID", nonEmpty, Kitty},
	{"TERM", oneOf("xterm-kitty", "xterm-ghostty"), Kitty},
	{"TERM_PROGRAM", oneOf("ghostty", "WezTerm"), Kitty},

	{"TERM", prefixOf("foot", "mlterm", "contour"), Sixel},
	{"TERM_PROGRAM", oneOf("contour", "iTerm.app"), Sixel},
}

func nonEmpty(v string) bool { return v != "" }

func oneOf(values ...string) func(string) bool {
	return func(v string) bool { return slices.Contains(values, v) }
}

func prefixOf(prefixes ...string) func(string) bool {
	return func(v string) bool {
		return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(v, p) })
	}
}

var protocolNames = map[string]Protocol{"halfblock": HalfBlock, "sixel": Sixel, "kitty": Kitty}

// String is the protocol's OMASLACK_IMAGES name.
func (p Protocol) String() string {
	for name, q := range protocolNames {
		if q == p {
			return name
		}
	}
	return "unknown"
}

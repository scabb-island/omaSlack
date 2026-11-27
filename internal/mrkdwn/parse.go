package mrkdwn

import "strings"

// Parse parses mrkdwn: *bold*, _italic_, ~strike~, `code`, ```blocks```,
// > and >>> quotes, <…> references (users, channels, <!…> commands and
// links), :emoji: (with ::skin-tone-N) and HTML entities. Control
// characters are dropped and tabs expanded first (Clean).
//
// custom resolves the workspace's custom emoji (nil: none known); a
// shortcode neither standard nor custom stays text, so "12:30:45" does.
func Parse(s string, custom func(name string) (string, bool)) []Block {
	p := parser{custom: custom}
	var out []Block
	for _, seg := range splitFences(Clean(s)) {
		if seg.code {
			out = append(out, CodeBlock{Text: seg.text})
			continue
		}
		out = append(out, p.lines(seg.text)...)
	}
	return out
}

type parser struct {
	custom func(name string) (string, bool)
}

// segment is a run of text between ``` fences, or the code inside them.
type segment struct {
	text string
	code bool
}

const fence = "```"

// splitFences cuts s at paired ``` fences. An unpaired fence is text. The
// newline right next to a fence belongs to the fence.
func splitFences(s string) []segment {
	var segs []segment
	for {
		open := strings.Index(s, fence)
		closing := -1
		if open >= 0 {
			closing = strings.Index(s[open+len(fence):], fence)
		}
		if closing < 0 {
			return appendText(segs, s)
		}
		segs = appendText(segs, strings.TrimSuffix(s[:open], "\n"))
		code := s[open+len(fence) : open+len(fence)+closing]
		code = strings.TrimSuffix(strings.TrimPrefix(code, "\n"), "\n")
		segs = append(segs, segment{text: code, code: true})
		s = strings.TrimPrefix(s[open+2*len(fence)+closing:], "\n")
	}
}

func appendText(segs []segment, s string) []segment {
	if s == "" {
		return segs
	}
	return append(segs, segment{text: s})
}

// lines parses each line, with "> " quotes and ">>>" (quote the rest).
func (p parser) lines(s string) []Block {
	var out []Block
	quoteRest := false
	for line := range strings.SplitSeq(s, "\n") {
		quoted := quoteRest
		if rest, ok := cutQuote(line, "&gt;&gt;&gt;", ">>>"); ok && !quoteRest {
			quoteRest, quoted, line = true, true, rest
		} else if rest, ok := cutQuote(line, "&gt;", ">"); ok {
			quoted, line = true, rest
		}
		out = append(out, Line{Quoted: quoted, Inlines: p.inline(line)})
	}
	return out
}

func cutQuote(line string, markers ...string) (string, bool) {
	for _, m := range markers {
		if rest, ok := strings.CutPrefix(line, m); ok {
			return strings.TrimPrefix(rest, " "), true
		}
	}
	return line, false
}

// inline parses one line. Text between markup is one Text, entities
// decoded.
func (p parser) inline(s string) []Inline {
	var out []Inline
	var text strings.Builder
	flush := func() {
		if t := unescape(text.String()); t != "" {
			out = append(out, Text{Text: t})
		}
		text.Reset()
	}
	for i := 0; i < len(s); {
		if in, n, ok := p.markup(s, i); ok {
			flush()
			out = append(out, in)
			i = n
			continue
		}
		text.WriteByte(s[i])
		i++
	}
	flush()
	return out
}

// markup tries the markup that starts at s[i]; n is where it ends.
func (p parser) markup(s string, i int) (Inline, int, bool) {
	switch s[i] {
	case '`':
		return codeSpan(s, i)
	case '<':
		return reference(s, i)
	case ':':
		return p.shortcode(s, i)
	}
	if mark, ok := markers[s[i]]; ok {
		return p.styled(s, i, mark)
	}
	return nil, 0, false
}

var markers = map[byte]Mark{'*': Bold, '_': Italic, '~': Strike}

func codeSpan(s string, i int) (Inline, int, bool) {
	j := strings.IndexByte(s[i+1:], '`')
	if j <= 0 {
		return nil, 0, false
	}
	return Code{Text: unescape(s[i+1 : i+1+j])}, i + j + 2, true
}

// styled follows Slack's rule: a marker opens after a non-word byte and
// before a non-space, and closes after a non-space and before a non-word
// byte, so snake_case and 2*3*4 stay text.
func (p parser) styled(s string, i int, mark Mark) (Inline, int, bool) {
	if !opens(s, i) {
		return nil, 0, false
	}
	for k := i + 2; k < len(s); k++ {
		if s[k] == s[i] && closes(s, k) {
			return Styled{Mark: mark, Inlines: p.inline(s[i+1 : k])}, k + 1, true
		}
	}
	return nil, 0, false
}

func opens(s string, i int) bool {
	return (i == 0 || !isWord(s[i-1])) && i+1 < len(s) && s[i+1] != ' '
}

func closes(s string, k int) bool {
	return s[k-1] != ' ' && (k+1 == len(s) || !isWord(s[k+1]))
}

// isWord treats non-ASCII bytes as word bytes so "año_x_" stays text.
func isWord(b byte) bool {
	return b >= 0x80 || b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// shortcode parses :name: when the name is known; an unknown one stays
// text.
func (p parser) shortcode(s string, i int) (Inline, int, bool) {
	j := strings.IndexFunc(s[i+1:], func(r rune) bool { return !isShortcode(r) })
	if j <= 0 || s[i+1+j] != ':' {
		return nil, 0, false
	}
	name := s[i+1 : i+1+j]
	text, ok := Emoji(name, p.custom)
	if !ok {
		return nil, 0, false
	}
	return Shortcode{Name: name, Text: text}, i + j + 2, true
}

func isShortcode(r rune) bool {
	return r == '_' || r == '-' || r == '+' || r == '\'' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z'
}

// reference parses <…>: users, channels, commands and links.
func reference(s string, i int) (Inline, int, bool) {
	j := strings.IndexByte(s[i+1:], '>')
	if j <= 0 {
		return nil, 0, false
	}
	target, label, _ := strings.Cut(s[i+1:i+1+j], "|")
	if target == "" {
		return nil, 0, false
	}
	return refOf(target, unescape(label)), i + j + 2, true
}

func refOf(target, label string) Inline {
	switch target[0] {
	case '@':
		return User{ID: target[1:], Label: label}
	case '#':
		return Channel{ID: target[1:], Label: label}
	case '!':
		return Special{Cmd: target[1:], Label: label}
	}
	return Link{URL: unescape(target), Label: label}
}

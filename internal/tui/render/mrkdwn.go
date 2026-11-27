package render

import (
	"html"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
)

// painter renders mrkdwn with one base style: the plain foreground, plus a
// background when the message is selected. Every other style derives from
// base, so the background survives the reset at the end of each run.
type painter struct {
	t      theme.Theme
	d      Directory
	base   lipgloss.Style
	filled bool // selected: blank cells get the background too
}

func newPainter(t theme.Theme, d Directory, selected bool) painter {
	base := lipgloss.NewStyle().Foreground(t.Foreground)
	if selected {
		base = base.Background(t.Selection)
	}
	return painter{t: t, d: d, base: base, filled: selected}
}

// emphasis is the set of *bold*, _italic_ and ~strike~ around a run.
type emphasis uint8

const (
	bold emphasis = 1 << iota
	italic
	strike
)

func (p painter) plain(e emphasis) lipgloss.Style {
	return p.base.Bold(e&bold != 0).Italic(e&italic != 0).Strikethrough(e&strike != 0)
}

// clean drops control characters (an ESC in a message must never reach the
// terminal) and expands tabs, keeping newlines.
func clean(s string) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// unescape decodes HTML entities; &#27; must not smuggle an ESC in.
func unescape(s string) string { return clean(html.UnescapeString(s)) }

// blocks renders cleaned mrkdwn as lines no wider than width (0: no limit).
func (p painter) blocks(s string, width int) []string {
	var out []string
	for _, seg := range splitFences(clean(s)) {
		if seg.code {
			out = append(out, p.codeBlock(seg.text, width)...)
			continue
		}
		out = append(out, p.textBlock(seg.text, width)...)
	}
	return out
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

// codeBlock keeps whitespace and never wraps: a line too long is cut.
func (p painter) codeBlock(s string, width int) []string {
	bar := p.base.Foreground(p.t.Code).Render("│ ")
	style := p.base.Foreground(p.t.Code)
	if width > 2 {
		style = style.MaxWidth(width - 2)
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = bar + style.Render(l)
	}
	return lines
}

// textBlock renders lines of inline mrkdwn, with "> " quotes and ">>>"
// (quote the rest).
func (p painter) textBlock(s string, width int) []string {
	var out []string
	quoteRest := false
	for line := range strings.SplitSeq(s, "\n") {
		quoted := quoteRest
		if rest, ok := cutQuote(line, "&gt;&gt;&gt;", ">>>"); ok && !quoteRest {
			quoteRest, quoted, line = true, true, rest
		} else if rest, ok := cutQuote(line, "&gt;", ">"); ok {
			quoted, line = true, rest
		}
		out = append(out, p.line(line, quoted, width)...)
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

func (p painter) line(s string, quoted bool, width int) []string {
	if !quoted {
		return wrap(p.inline(s, 0), width)
	}
	bar := p.base.Foreground(p.t.Quote).Render("▎ ")
	lines := wrap(p.inline(s, 0), max(width-2, 0))
	for i, l := range lines {
		lines[i] = bar + l
	}
	return lines
}

// wrap breaks at spaces and, for a word longer than the line, inside the
// word. Widths count terminal cells, so CJK and emoji take two.
func wrap(s string, width int) []string {
	if width > 0 {
		s = ansi.Hardwrap(ansi.Wordwrap(s, width, ""), width, true)
	}
	return strings.Split(s, "\n")
}

// inline renders one line: emphasis, `code`, <…> references and :emoji:.
// Text that is not markup is printed as is, entities decoded.
func (p painter) inline(s string, e emphasis) string {
	var out, text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out.WriteString(p.plain(e).Render(unescape(text.String())))
			text.Reset()
		}
	}
	for i := 0; i < len(s); {
		if r, n, ok := p.markup(s, i, e); ok {
			flush()
			out.WriteString(r)
			i = n
			continue
		}
		text.WriteByte(s[i])
		i++
	}
	flush()
	return out.String()
}

// markup tries the markup that starts at s[i]; n is where it ends.
func (p painter) markup(s string, i int, e emphasis) (string, int, bool) {
	switch s[i] {
	case '`':
		return p.codeSpan(s, i)
	case '<':
		return p.reference(s, i)
	case ':':
		return p.emoji(s, i)
	}
	if mark, ok := markers[s[i]]; ok {
		return p.emphasized(s, i, e|mark)
	}
	return "", 0, false
}

var markers = map[byte]emphasis{'*': bold, '_': italic, '~': strike}

func (p painter) codeSpan(s string, i int) (string, int, bool) {
	j := strings.IndexByte(s[i+1:], '`')
	if j <= 0 {
		return "", 0, false
	}
	code := unescape(s[i+1 : i+1+j])
	return p.base.Foreground(p.t.Code).Render(code), i + j + 2, true
}

// emphasized follows Slack's rule: a marker opens after a non-word byte
// and before a non-space, and closes after a non-space and before a
// non-word byte, so snake_case and 2*3*4 stay text.
func (p painter) emphasized(s string, i int, e emphasis) (string, int, bool) {
	if !opens(s, i) {
		return "", 0, false
	}
	for k := i + 2; k < len(s); k++ {
		if s[k] == s[i] && closes(s, k) {
			return p.inline(s[i+1:k], e), k + 1, true
		}
	}
	return "", 0, false
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

// emoji renders :name: when the name is known; an unknown one stays text.
func (p painter) emoji(s string, i int) (string, int, bool) {
	j := strings.IndexFunc(s[i+1:], func(r rune) bool { return !isShortcode(r) })
	if j <= 0 || s[i+1+j] != ':' {
		return "", 0, false
	}
	name := s[i+1 : i+1+j]
	text, ok := emojiText(name, p.d)
	if !ok {
		return "", 0, false
	}
	if _, std := StandardEmoji(name); !std {
		return p.base.Foreground(p.t.Accent).Render(text), i + j + 2, true
	}
	return p.base.Render(text), i + j + 2, true
}

func isShortcode(r rune) bool {
	return r == '_' || r == '-' || r == '+' || r == '\'' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z'
}

// reference renders <…>: users, channels, broadcasts and links.
func (p painter) reference(s string, i int) (string, int, bool) {
	j := strings.IndexByte(s[i+1:], '>')
	if j <= 0 {
		return "", 0, false
	}
	target, label, _ := strings.Cut(s[i+1:i+1+j], "|")
	if target == "" {
		return "", 0, false
	}
	label = unescape(label)
	var r string
	switch target[0] {
	case '@':
		r = p.user(target[1:], label)
	case '#':
		r = p.channel(target[1:], label)
	case '!':
		r = p.special(target[1:], label)
	default:
		r = p.link(unescape(target), label)
	}
	return r, i + j + 2, true
}

func (p painter) user(id, label string) string {
	name := firstOf(clean(p.d.UserName(id)), label, id)
	style := p.base.Foreground(p.t.Accent).Bold(true)
	if id == p.d.Self() {
		style = p.base.Foreground(p.t.Mention).Bold(true)
	}
	return style.Render("@" + name)
}

func (p painter) channel(id, label string) string {
	name := firstOf(label, clean(p.d.ConversationName(id)), id)
	return p.base.Foreground(p.t.Accent).Render("#" + name)
}

// special renders <!…>: broadcasts notify everyone, the user included.
func (p painter) special(cmd, label string) string {
	name, _, _ := strings.Cut(cmd, "^")
	switch name {
	case "here", "channel", "everyone":
		return p.base.Foreground(p.t.Mention).Bold(true).Render("@" + name)
	case "subteam":
		_, id, _ := strings.Cut(cmd, "^")
		return p.base.Foreground(p.t.Accent).Bold(true).Render(firstOf(label, "@"+id))
	case "date":
		return p.base.Render(firstOf(label, cmd))
	}
	return p.base.Render(firstOf(label, "@"+name))
}

// link shows the label (or the URL) as an OSC 8 hyperlink, so terminals
// that support it open the URL on click. A target that does not look like
// a URL is shown without one: a space would let wrapping split the escape.
func (p painter) link(url, label string) string {
	text := firstOf(label, strings.TrimPrefix(url, "mailto:"))
	style := p.base.Foreground(p.t.Accent)
	if isURL(url) {
		style = style.Hyperlink(url)
	}
	return style.Render(text)
}

func isURL(s string) bool {
	return (strings.Contains(s, "://") || strings.HasPrefix(s, "mailto:")) && !strings.ContainsFunc(s, unicode.IsSpace)
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

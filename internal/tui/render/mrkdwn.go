package render

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"gitlab.com/scabb-island/omaslack/internal/mrkdwn"
	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
)

// painter paints mrkdwn's tree with one base style: the plain foreground,
// plus a background when the message is selected. Every other style
// derives from base, so the background survives the reset at the end of
// each run.
type painter struct {
	t      theme.Theme
	d      Directory
	base   style
	filled bool // selected: blank cells get the background too
}

func newPainter(t theme.Theme, d Directory, selected bool) painter {
	base := style{}.Foreground(t.Foreground)
	if selected {
		base = base.Background(t.Selection)
	}
	return painter{t: t, d: d, base: base, filled: selected}
}

// oneLine is for what must stay on one line: names from the Directory
// and from wire values.
func oneLine(s string) string { return mrkdwn.OneLine(s) }

// blocks renders mrkdwn as lines no wider than width (0: no limit).
func (p painter) blocks(s string, width int) []string {
	var out []string
	for _, b := range mrkdwn.Parse(s, p.d.Emoji) {
		switch b := b.(type) {
		case mrkdwn.CodeBlock:
			out = append(out, p.codeBlock(b.Text, width)...)
		case mrkdwn.Line:
			out = append(out, p.line(b, width)...)
		}
	}
	return out
}

// codeBlock keeps whitespace and never wraps: a line too long is cut.
func (p painter) codeBlock(s string, width int) []string {
	code := p.base.Foreground(p.t.Code)
	bar := code.Render("│ ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if width > 2 {
			l = ansi.Truncate(l, width-2, "")
		}
		lines[i] = bar + code.Render(l)
	}
	return lines
}

// line wraps one line; a quoted one gets a bar in front of each row.
func (p painter) line(l mrkdwn.Line, width int) []string {
	text := mrkdwn.Render(l.Inlines, inliner{painter: p})
	if !l.Quoted {
		return wrap(text, width)
	}
	bar := p.base.Foreground(p.t.Quote).Render("▎ ")
	lines := wrap(text, max(width-2, 0))
	for i, row := range lines {
		lines[i] = bar + row
	}
	return lines
}

// inliner is the mrkdwn.Visitor that paints a line's inlines under the
// emphasis around them.
type inliner struct {
	painter
	e mrkdwn.Mark
}

var _ mrkdwn.Visitor = inliner{}

func (v inliner) Text(t mrkdwn.Text) string {
	return v.base.Bold(v.e&mrkdwn.Bold != 0).Italic(v.e&mrkdwn.Italic != 0).Strikethrough(v.e&mrkdwn.Strike != 0).Render(t.Text)
}

func (v inliner) Code(c mrkdwn.Code) string { return v.base.Foreground(v.t.Code).Render(c.Text) }

func (v inliner) Styled(s mrkdwn.Styled) string {
	return mrkdwn.Render(s.Inlines, inliner{painter: v.painter, e: v.e | s.Mark})
}

// Shortcode prints standard emoji plainly and custom ones (and names with
// a skin tone) in the accent color.
func (v inliner) Shortcode(s mrkdwn.Shortcode) string {
	if _, std := mrkdwn.StandardEmoji(s.Name); !std {
		return v.base.Foreground(v.t.Accent).Render(s.Text)
	}
	return v.base.Render(s.Text)
}

func (v inliner) User(u mrkdwn.User) string {
	name := firstOf(oneLine(v.d.UserName(u.ID)), u.Label, u.ID)
	style := v.base.Foreground(v.t.Accent).Bold(true)
	if u.ID == v.d.Self() {
		style = v.base.Foreground(v.t.Mention).Bold(true)
	}
	return style.Render("@" + name)
}

func (v inliner) Channel(c mrkdwn.Channel) string {
	name := firstOf(c.Label, oneLine(v.d.ConversationName(c.ID)), c.ID)
	return v.base.Foreground(v.t.Accent).Render("#" + name)
}

// Special renders <!…>: broadcasts notify everyone, the user included.
func (v inliner) Special(s mrkdwn.Special) string {
	switch s.Name() {
	case "here", "channel", "everyone":
		return v.base.Foreground(v.t.Mention).Bold(true).Render("@" + s.Name())
	case "subteam":
		return v.base.Foreground(v.t.Accent).Bold(true).Render(firstOf(s.Label, "@"+s.Arg()))
	case "date":
		return v.base.Render(firstOf(s.Label, s.Cmd))
	}
	return v.base.Render(firstOf(s.Label, "@"+s.Name()))
}

// Link shows the label (or the URL) as an OSC 8 hyperlink, so terminals
// that support it open the URL on click. A target that does not look like
// a URL is shown without one: a space would let wrapping split the escape.
func (v inliner) Link(l mrkdwn.Link) string {
	text := firstOf(l.Label, strings.TrimPrefix(l.URL, "mailto:"))
	style := v.base.Foreground(v.t.Accent)
	if isURL(l.URL) {
		style = style.Hyperlink(l.URL)
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

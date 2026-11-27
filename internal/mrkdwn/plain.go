package mrkdwn

import (
	"html"
	"strings"
)

// Plain renders mrkdwn as plain text, for a desktop notification's body:
// emphasis marks dropped, entities decoded, mentions and channels by
// name ("@alice", "#general"), links as their label, emoji as Unicode,
// quoted lines kept behind "> " and code blocks as their text. Nothing is
// escaped; control characters are dropped. names may be nil.
func Plain(mrkdwn string, names Names) string {
	v := plainVisitor{names: names}
	if names == nil {
		v.names = noNames{}
	}
	var lines []string
	for _, blk := range Parse(mrkdwn, nil) {
		lines = append(lines, v.block(blk))
	}
	return strings.Join(lines, "\n")
}

type plainVisitor struct{ names Names }

var _ Visitor = plainVisitor{}

func (v plainVisitor) block(b Block) string {
	l, ok := b.(Line)
	switch {
	case !ok:
		return Clean(html.UnescapeString(b.(CodeBlock).Text))
	case l.Quoted:
		return "> " + Render(l.Inlines, v)
	default:
		return Render(l.Inlines, v)
	}
}

func (plainVisitor) Text(t Text) string { return t.Text }

func (plainVisitor) Code(c Code) string { return c.Text }

func (v plainVisitor) Styled(s Styled) string { return Render(s.Inlines, v) }

func (v plainVisitor) User(u User) string { return "@" + userName(v.names, u) }

func (v plainVisitor) Channel(c Channel) string { return "#" + channelName(v.names, c) }

func (plainVisitor) Special(s Special) string { return s.Text() }

func (plainVisitor) Link(l Link) string { return l.Text() }

func (plainVisitor) Shortcode(s Shortcode) string { return s.Text }

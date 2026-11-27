// Package mrkdwn parses Slack's mrkdwn into a small tree, once, for every
// renderer: the TUI paints it with terminal styles (internal/tui/render)
// and the daemon renders it to the HTML subset Qt's Text.RichText draws
// (HTML), so the QML plugin never parses mrkdwn (spec gui-client, "Long
// histories without degrading the shell"; task 4.9).
//
// The tree holds what the text says, not how it looks: names behind
// <@U…> and <#C…> are resolved by each renderer, from its own directory.
// Malformed markup is text, never an error.
package mrkdwn

import (
	"cmp"
	"strings"
)

// Block is one line of text (Line) or one fenced code block (CodeBlock).
type Block interface{ isBlock() }

// Line is one line of inline mrkdwn, quoted by a leading "> " or by an
// earlier ">>>" (which quotes the rest of the text).
type Line struct {
	Quoted  bool
	Inlines []Inline
}

// CodeBlock is the text between ``` fences, whitespace kept. Its HTML
// entities are left as Slack sent them (&lt; stays &lt;).
type CodeBlock struct{ Text string }

func (Line) isBlock()      {}
func (CodeBlock) isBlock() {}

// Inline is one piece of a line. Accept hands it to the visitor method of
// its kind.
type Inline interface {
	Accept(v Visitor) string
}

// Visitor renders each kind of Inline.
type Visitor interface {
	Text(Text) string
	Code(Code) string
	Styled(Styled) string
	User(User) string
	Channel(Channel) string
	Special(Special) string
	Link(Link) string
	Shortcode(Shortcode) string
}

// Text is plain text, entities decoded, on one line.
type Text struct{ Text string }

// Code is a `code span`, entities decoded.
type Code struct{ Text string }

// Mark is one kind of emphasis.
type Mark uint8

// The emphasis marks: *bold*, _italic_ and ~strike~.
const (
	Bold Mark = 1 << iota
	Italic
	Strike
)

// Styled is a run of inlines inside one emphasis marker; nested markers
// nest Styled.
type Styled struct {
	Mark    Mark
	Inlines []Inline
}

// User is a <@U…> mention; Label is the legacy <@U…|name> name, if any.
type User struct{ ID, Label string }

// Channel is a <#C…> reference; Label is <#C…|name>'s name, if any.
type Channel struct{ ID, Label string }

// Special is a <!…> command: <!here>, <!channel>, <!everyone>,
// <!subteam^S…|@group>, <!date^…|fallback> and the like. Cmd is what
// follows the "!", Label what follows the "|".
type Special struct{ Cmd, Label string }

// Name is the command without its arguments ("subteam" for
// "subteam^S1").
func (s Special) Name() string { name, _, _ := strings.Cut(s.Cmd, "^"); return name }

// Arg is what follows the command's first "^" ("S1" for "subteam^S1").
func (s Special) Arg() string { _, arg, _ := strings.Cut(s.Cmd, "^"); return arg }

// Link is <url> or <url|label>, both entity-decoded. URL is whatever the
// text says: a renderer decides whether it is safe to follow.
type Link struct{ URL, Label string }

// Shortcode is a known :emoji: (with ::skin-tone-N); Text is what it
// resolved to. An unknown one is Text.
type Shortcode struct{ Name, Text string }

func (t Text) Accept(v Visitor) string      { return v.Text(t) }
func (c Code) Accept(v Visitor) string      { return v.Code(c) }
func (s Styled) Accept(v Visitor) string    { return v.Styled(s) }
func (u User) Accept(v Visitor) string      { return v.User(u) }
func (c Channel) Accept(v Visitor) string   { return v.Channel(c) }
func (s Special) Accept(v Visitor) string   { return v.Special(s) }
func (l Link) Accept(v Visitor) string      { return v.Link(l) }
func (s Shortcode) Accept(v Visitor) string { return v.Shortcode(s) }

// Loud reports whether the command reaches people: a broadcast (here,
// channel, everyone) or a user group.
func (s Special) Loud() bool {
	switch s.Name() {
	case "here", "channel", "everyone", "subteam":
		return true
	}
	return false
}

// Text is what the command reads as: "@here", the group's label (or
// "@<id>"), a date's fallback, or else its label or "@<name>".
func (s Special) Text() string {
	switch s.Name() {
	case "here", "channel", "everyone":
		return "@" + s.Name()
	case "subteam":
		return cmp.Or(s.Label, "@"+s.Arg())
	case "date":
		return cmp.Or(s.Label, s.Cmd)
	}
	return cmp.Or(s.Label, "@"+s.Name())
}

// Text is what the link reads as: its label, or its URL without "mailto:".
func (l Link) Text() string { return cmp.Or(l.Label, strings.TrimPrefix(l.URL, "mailto:")) }

// userName is how a mention of u reads (without the "@"): its name in
// names, the label it carries, or its ID.
func userName(names Names, u User) string {
	return cmp.Or(OneLine(names.UserName(u.ID)), u.Label, u.ID)
}

// channelName is how a reference to c reads (without the "#"): the label
// it carries, its name in names, or its ID.
func channelName(names Names, c Channel) string {
	return cmp.Or(c.Label, OneLine(names.ConversationName(c.ID)), c.ID)
}

// Render visits each inline in order and joins what v returns.
func Render(inlines []Inline, v Visitor) string {
	var b strings.Builder
	for _, in := range inlines {
		b.WriteString(in.Accept(v))
	}
	return b.String()
}

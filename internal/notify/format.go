package notify

import (
	"cmp"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/mrkdwn"
)

// Details is a Candidate with the names its notification shows.
type Details struct {
	Candidate
	Workspace string // the workspace's name, shown when set (several workspaces)
	Author    string // the author's display name; "" when unknown
	// Names names the people and channels the text mentions; nil names
	// nobody.
	Names mrkdwn.Names
}

// Notification is what a Sender shows.
type Notification struct {
	Summary string // plain text
	Body    string // plain text, with &, < and > escaped as the spec's body markup asks
}

// maxBody is how many characters of a message the body shows.
const maxBody = 300

// Format is the notification of d: who wrote it where as the summary
// ("Alice", "Alice in #general (thread)"), the message as plain text as
// the body.
func Format(d Details) Notification {
	return Notification{Summary: summary(d), Body: body(d)}
}

func summary(d Details) string {
	s := cmp.Or(mrkdwn.OneLine(d.Author), "Someone") + where(d.Conversation)
	if d.Message.IsReply() {
		s += " (thread)"
	}
	if d.Workspace != "" {
		s += " (" + mrkdwn.OneLine(d.Workspace) + ")"
	}
	return s
}

func where(c domain.Conversation) string {
	switch c.Kind {
	case domain.DirectMessage:
		return ""
	case domain.GroupMessage:
		return " in a group DM"
	}
	return " in #" + mrkdwn.OneLine(cmp.Or(c.Name, c.ID.String()))
}

var bodyEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func body(d Details) string {
	text := strings.TrimSpace(mrkdwn.Plain(d.Message.Text, d.Names))
	if text == "" && len(d.Message.Files) > 0 {
		text = "Sent a file"
	}
	if r := []rune(text); len(r) > maxBody {
		text = string(r[:maxBody]) + "…"
	}
	return bodyEscaper.Replace(text)
}

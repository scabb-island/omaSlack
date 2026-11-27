// Package wire is the JSON shape of everything the daemon sends clients
// over the local protocol (spec daemon-api): method results and event
// notifications. Both clients (the TUI and the QML plugin) read exactly
// these shapes, so they change only together with version.Protocol.
//
// IDs and timestamps travel as Slack's own strings; an empty optional
// value is omitted.
package wire

import (
	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// Message is one message.
type Message struct {
	TS       string `json:"ts"`
	ThreadTS string `json:"threadTS,omitempty"`
	Author   string `json:"author,omitempty"`
	Text     string `json:"text"`
	// HTML is Text rendered for Qt's Text.RichText (mrkdwn parsed in the
	// daemon, mentions and channels resolved to names, emoji shortcodes
	// to characters), so QML never parses mrkdwn (spec gui-client, "Long
	// histories without degrading the shell"). Empty until task 4.9 fills
	// it; clients fall back to Text.
	HTML   string `json:"html,omitempty"`
	Edited string `json:"edited,omitempty"`
	// A thread root's replies: how many (omitted when none or unknown)
	// and the newest one's ts (omitted when unknown).
	ReplyCount  int        `json:"replyCount,omitempty"`
	LatestReply string     `json:"latestReply,omitempty"`
	Reactions   []Reaction `json:"reactions,omitempty"`
	Files       []File     `json:"files,omitempty"`
}

// File is one file attached to a message: what a client shows. Its bytes
// are fetched through the daemon (files.fetch), never from Slack.
type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Title    string `json:"title,omitempty"`
	MIMEType string `json:"mimetype,omitempty"`
	Size     int64  `json:"size"`
}

// FromFile converts one file. Its download URL stays in the daemon.
func FromFile(f domain.File) File {
	return File{ID: f.ID.String(), Name: f.Name, Title: f.Title, MIMEType: f.MIMEType, Size: f.Size}
}

// Reaction is one emoji on a message and who added it.
type Reaction struct {
	Emoji string   `json:"emoji"`
	Users []string `json:"users"`
}

// Conversation is one entry of a workspace's conversation list.
type Conversation struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	Latest   string `json:"latest,omitempty"`
	LastRead string `json:"lastRead,omitempty"`
	Mentions int    `json:"mentions"`
	Unread   bool   `json:"unread"`
	User     string `json:"user,omitempty"` // the other person of an "im"
}

// Workspace is one signed-in workspace, as workspaces.list and hello
// report it.
type Workspace struct {
	Team         string   `json:"team"`
	Name         string   `json:"name"`
	Self         string   `json:"self,omitempty"` // the signed-in user
	Provider     string   `json:"provider"`
	Capabilities []string `json:"capabilities"`
	State        string   `json:"state"`
}

// User is one person, as users.search and users.info return them.
type User struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RealName    string `json:"realName,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// SearchHit is one message search.messages found.
type SearchHit struct {
	Conv    string  `json:"conv"`
	Message Message `json:"message"`
}

// FromMessage converts a message with its reactions (nil for none).
func FromMessage(m domain.Message, reactions []store.Reaction) Message {
	out := Message{
		TS:       m.TS.String(),
		ThreadTS: optTS(m.ThreadTS),
		Author:   m.Author.String(),
		Text:     m.Text,
		Edited:   optTS(m.Edited),

		ReplyCount:  m.ReplyCount,
		LatestReply: optTS(m.LatestReply),
	}
	for _, r := range reactions {
		out.Reactions = append(out.Reactions, Reaction{Emoji: r.Emoji, Users: ids(r.Users)})
	}
	for _, f := range m.Files {
		out.Files = append(out.Files, FromFile(f))
	}
	return out
}

// FromConversation converts one conversation list entry.
func FromConversation(c domain.Conversation) Conversation {
	return Conversation{
		ID:       c.ID.String(),
		Kind:     c.Kind.String(),
		Name:     c.Name,
		Latest:   optTS(c.Latest),
		LastRead: optTS(c.LastRead),
		Mentions: c.Mentions,
		Unread:   c.Unread(),
		User:     optUser(c.User),
	}
}

// FromWorkspace converts a workspace's identity and state. A zero self
// (who is signed in is unknown) is omitted.
func FromWorkspace(team domain.TeamID, name string, self domain.UserID, provider string, caps domain.Capabilities, state domain.ConnectionState) Workspace {
	names := make([]string, 0, len(caps.List()))
	for _, c := range caps.List() {
		names = append(names, c.String())
	}
	return Workspace{Team: team.String(), Name: name, Self: optUser(self), Provider: provider, Capabilities: names, State: state.String()}
}

// FromUser converts one user.
func FromUser(u domain.User) User {
	return User{ID: u.ID.String(), Name: u.Name, RealName: u.RealName, DisplayName: u.DisplayName}
}

// FromSearchHit converts one message search hit.
func FromSearchHit(h domain.SearchHit) SearchHit {
	return SearchHit{Conv: h.Conversation.String(), Message: FromMessage(h.Message, nil)}
}

func optTS(t domain.TS) string { return t.OrEmpty() }

func ids(users []domain.UserID) []string {
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.String()
	}
	return out
}

func optUser(u domain.UserID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

// EmojiList is the result of emoji.list: the workspace's custom emoji, by
// name (without colons). A value is an image URL the daemon resolved, or
// "alias:<name>" for an alias of another emoji (custom or standard).
type EmojiList struct {
	Emoji map[string]string `json:"emoji"`
}

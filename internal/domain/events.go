package domain

import "time"

// Message is one Slack message as the daemon stores and serves it.
//
// Files makes Message (and the events that carry one) not comparable with
// ==; compare with reflect.DeepEqual.
type Message struct {
	TS       TS
	ThreadTS TS // zero when the message is not part of a thread
	Author   Author
	Text     string // Slack mrkdwn, as received
	Edited   TS     // zero when never edited
	Files    []File // attachments in Slack's order; nil for none

	// A thread root's replies: how many (0 when none or unknown) and the
	// newest one's ts (zero when unknown). Replies leave both zero.
	ReplyCount  int
	LatestReply TS
}

// IsReply reports whether m is a reply inside a thread (a thread root is
// not a reply).
func (m Message) IsReply() bool { return !m.ThreadTS.IsZero() && m.ThreadTS != m.TS }

// Event is a normalized domain event, the only thing providers emit and the
// sync engine consumes. Its topic is the name clients subscribe to (spec
// daemon-api, "Event subscription"). The interface is sealed: only this
// package defines events.
type Event interface {
	Topic() string
	TeamID() TeamID
	sealed()
}

// MessageCreated: a new message in a conversation the user can see.
type MessageCreated struct {
	Team         TeamID
	Conversation ConversationID
	Message      Message
}

// MessageUpdated: a message was edited.
type MessageUpdated struct {
	Team         TeamID
	Conversation ConversationID
	Message      Message
}

// MessageDeleted: a message was deleted.
type MessageDeleted struct {
	Team         TeamID
	Conversation ConversationID
	TS           TS
}

// ReactionChanged: someone added or removed a reaction.
type ReactionChanged struct {
	Team         TeamID
	Conversation ConversationID
	TS           TS
	User         UserID
	Emoji        string
	Added        bool
}

// ReadChanged: the user's read marker moved in a conversation.
type ReadChanged struct {
	Team         TeamID
	Conversation ConversationID
	LastRead     TS
}

// Presence is a user's Slack presence.
type Presence uint8

const (
	Away Presence = iota
	Active
)

// PresenceChanged: a user went active or away.
type PresenceChanged struct {
	Team     TeamID
	User     UserID
	Presence Presence
}

// DNDChanged: the signed-in user's Do Not Disturb changed (snoozed until
// Until, or ended when Until is zero), from any client or Slack itself.
type DNDChanged struct {
	Team  TeamID
	Until time.Time
}

// UserTyping: someone is typing in a conversation.
type UserTyping struct {
	Team         TeamID
	Conversation ConversationID
	User         UserID
}

// ConnectionState is the state of one workspace's connection to Slack
// (spec sync-engine, "Reconnection with backoff").
type ConnectionState uint8

const (
	Connecting ConnectionState = iota
	Connected
	Disconnected
	RateLimited
	AuthExpired
)

var connectionStateNames = [...]string{
	Connecting:   "connecting",
	Connected:    "connected",
	Disconnected: "disconnected",
	RateLimited:  "rate_limited",
	AuthExpired:  "auth_expired",
}

func (s ConnectionState) String() string { return connectionStateNames[s] }

// ConnectionChanged: a workspace's connection changed state.
type ConnectionChanged struct {
	Team  TeamID
	State ConnectionState
}

// ConversationUpdated: a conversation's entry in the list changed (its
// latest message, read marker or unread mentions). Conversation carries the
// whole entry as the daemon now knows it.
type ConversationUpdated struct {
	Team         TeamID
	Conversation Conversation
}

// DraftChanged: a client saved the unsent draft of a conversation, or of
// the thread rooted at Thread when it is not zero. An empty Text means the
// draft was deleted.
type DraftChanged struct {
	Team         TeamID
	Conversation ConversationID
	Thread       TS
	Text         string
}

func (MessageCreated) Topic() string      { return "message.created" }
func (MessageUpdated) Topic() string      { return "message.updated" }
func (MessageDeleted) Topic() string      { return "message.deleted" }
func (ReactionChanged) Topic() string     { return "reaction.changed" }
func (ReadChanged) Topic() string         { return "read.changed" }
func (PresenceChanged) Topic() string     { return "presence.changed" }
func (UserTyping) Topic() string          { return "typing" }
func (DNDChanged) Topic() string          { return "dnd.changed" }
func (ConnectionChanged) Topic() string   { return "connection.state" }
func (ConversationUpdated) Topic() string { return "conversation.updated" }
func (DraftChanged) Topic() string        { return "draft.changed" }

func (e MessageCreated) TeamID() TeamID      { return e.Team }
func (e MessageUpdated) TeamID() TeamID      { return e.Team }
func (e MessageDeleted) TeamID() TeamID      { return e.Team }
func (e ReactionChanged) TeamID() TeamID     { return e.Team }
func (e ReadChanged) TeamID() TeamID         { return e.Team }
func (e PresenceChanged) TeamID() TeamID     { return e.Team }
func (e UserTyping) TeamID() TeamID          { return e.Team }
func (e DNDChanged) TeamID() TeamID          { return e.Team }
func (e ConnectionChanged) TeamID() TeamID   { return e.Team }
func (e ConversationUpdated) TeamID() TeamID { return e.Team }
func (e DraftChanged) TeamID() TeamID        { return e.Team }

func (MessageCreated) sealed()      {}
func (MessageUpdated) sealed()      {}
func (MessageDeleted) sealed()      {}
func (ReactionChanged) sealed()     {}
func (DNDChanged) sealed()          {}
func (ReadChanged) sealed()         {}
func (PresenceChanged) sealed()     {}
func (UserTyping) sealed()          {}
func (ConnectionChanged) sealed()   {}
func (ConversationUpdated) sealed() {}
func (DraftChanged) sealed()        {}

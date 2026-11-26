package wire

import "gitlab.com/scabb-island/omaslack/internal/domain"

// Event notifications: the method of each JSON-RPC notification is the
// event's topic (domain.Event.Topic) and its params are one of these.

// MessageEvent is the payload of message.created and message.updated.
type MessageEvent struct {
	Team    string  `json:"team"`
	Conv    string  `json:"conv"`
	Message Message `json:"message"`
}

// MessageDeletedEvent is the payload of message.deleted.
type MessageDeletedEvent struct {
	Team string `json:"team"`
	Conv string `json:"conv"`
	TS   string `json:"ts"`
}

// ReactionEvent is the payload of reaction.changed.
type ReactionEvent struct {
	Team  string `json:"team"`
	Conv  string `json:"conv"`
	TS    string `json:"ts"`
	User  string `json:"user"`
	Emoji string `json:"emoji"`
	Added bool   `json:"added"`
}

// ReadEvent is the payload of read.changed.
type ReadEvent struct {
	Team     string `json:"team"`
	Conv     string `json:"conv"`
	LastRead string `json:"lastRead"`
}

// PresenceEvent is the payload of presence.changed.
type PresenceEvent struct {
	Team     string `json:"team"`
	User     string `json:"user"`
	Presence string `json:"presence"` // "active" or "away"
}

// TypingEvent is the payload of typing.
type TypingEvent struct {
	Team string `json:"team"`
	Conv string `json:"conv"`
	User string `json:"user"`
}

// ConnectionEvent is the payload of connection.state.
type ConnectionEvent struct {
	Team  string `json:"team"`
	State string `json:"state"`
}

// ConversationEvent is the payload of conversation.updated.
type ConversationEvent struct {
	Team         string       `json:"team"`
	Conversation Conversation `json:"conversation"`
}

// DraftEvent is the payload of draft.changed. An empty Text means the
// draft was deleted.
type DraftEvent struct {
	Team     string `json:"team"`
	Conv     string `json:"conv"`
	ThreadTS string `json:"threadTS,omitempty"`
	Text     string `json:"text"`
}

// FocusEvent is the payload of ui.focus and the params of the ui.focus
// method: a request for a client to show a conversation.
type FocusEvent struct {
	Team string `json:"team"`
	Conv string `json:"conv"`
	TS   string `json:"ts,omitempty"`
}

// FromEvent converts a domain event to its notification payload. Message
// events carry no reactions: those arrive as reaction.changed.
func FromEvent(ev domain.Event) any {
	if p, ok := fromMessageEvent(ev); ok {
		return p
	}
	return fromStateEvent(ev)
}

// fromMessageEvent converts the events about messages and their reactions.
func fromMessageEvent(ev domain.Event) (any, bool) {
	team := ev.TeamID().String()
	switch e := ev.(type) {
	case domain.MessageCreated:
		return MessageEvent{Team: team, Conv: e.Conversation.String(), Message: FromMessage(e.Message, nil)}, true
	case domain.MessageUpdated:
		return MessageEvent{Team: team, Conv: e.Conversation.String(), Message: FromMessage(e.Message, nil)}, true
	case domain.MessageDeleted:
		return MessageDeletedEvent{Team: team, Conv: e.Conversation.String(), TS: e.TS.String()}, true
	case domain.ReactionChanged:
		return ReactionEvent{Team: team, Conv: e.Conversation.String(), TS: e.TS.String(), User: e.User.String(), Emoji: e.Emoji, Added: e.Added}, true
	}
	return nil, false
}

// fromStateEvent converts the rest: read state, people, connection,
// conversations and drafts.
func fromStateEvent(ev domain.Event) any {
	team := ev.TeamID().String()
	switch e := ev.(type) {
	case domain.ReadChanged:
		return ReadEvent{Team: team, Conv: e.Conversation.String(), LastRead: e.LastRead.String()}
	case domain.PresenceChanged:
		return PresenceEvent{Team: team, User: e.User.String(), Presence: presence(e.Presence)}
	case domain.UserTyping:
		return TypingEvent{Team: team, Conv: e.Conversation.String(), User: e.User.String()}
	case domain.ConnectionChanged:
		return ConnectionEvent{Team: team, State: e.State.String()}
	case domain.ConversationUpdated:
		return ConversationEvent{Team: team, Conversation: FromConversation(e.Conversation)}
	case domain.DraftChanged:
		return DraftEvent{Team: team, Conv: e.Conversation.String(), ThreadTS: optTS(e.Thread), Text: e.Text}
	}
	panic("wire: unknown event " + ev.Topic()) // the interface is sealed
}

func presence(p domain.Presence) string {
	if p == domain.Active {
		return "active"
	}
	return "away"
}

package slackwire

import (
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Slack events → domain events, for the event shapes that the Events API
// and the web client's socket share.
//
// Mapped: message (no subtype, thread_broadcast, file_share, me_message,
// bot_message) → MessageCreated; message_changed → MessageUpdated;
// message_deleted → MessageDeleted; reaction_added and reaction_removed on
// messages, or on an item with no type → ReactionChanged.
//
// Every other message subtype (channel_join, channel_topic, …) has no
// mapper, and callers ignore it. A reaction on any other item (a file)
// is an ErrUnsupportedReactionItem, which callers log and drop.

// Head is what every event shares: enough to pick its mapper before
// decoding the fields, whose shapes differ between event types.
type Head struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

// Kind is the event type, plus the subtype for messages. Mappers are
// keyed by it.
func (h Head) Kind() string {
	if h.Type == "message" {
		return "message/" + h.Subtype
	}
	return h.Type
}

// Event holds the fields of every event type mapped here. The embedded
// Message is the top-level message of a new-message event; its user is
// also the user of a reaction, typing or presence event, and its ts the
// read marker of a *_marked event.
type Event struct {
	Head
	Message
	Channel   string  `json:"channel"`
	Edit      Message `json:"message"` // the new version, in message_changed
	DeletedTS string  `json:"deleted_ts"`
	Reaction  string  `json:"reaction"`
	Item      struct {
		Type    string `json:"type"`
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	} `json:"item"`
}

// Mapper builds the domain event for one kind of Event. It returns nil and
// no error for an event the domain does not track, and an error for a
// malformed one, so no half-valid event is ever emitted.
type Mapper func(domain.TeamID, Event) (domain.Event, error)

// mappers is keyed by Head.Kind.
var mappers = map[string]Mapper{
	"message/":                 messageCreated,
	"message/thread_broadcast": messageCreated,
	"message/file_share":       messageCreated,
	"message/me_message":       messageCreated,
	"message/bot_message":      messageCreated,
	"message/message_changed":  messageUpdated,
	"message/message_deleted":  messageDeleted,
	"reaction_added":           reactionChanged(true),
	"reaction_removed":         reactionChanged(false),
}

// MapperFor returns the mapper of kind, or false when no shared mapper
// handles that kind of event.
func MapperFor(kind string) (Mapper, bool) {
	m, ok := mappers[kind]
	return m, ok
}

// messageEvent builds the mapper of an event that carries a whole message:
// at says where the event keeps it and wrap builds the domain event.
func messageEvent(at func(Event) Message, wrap func(domain.TeamID, domain.ConversationID, domain.Message) domain.Event) Mapper {
	return func(team domain.TeamID, e Event) (domain.Event, error) {
		conv, err := domain.ParseConversationID(e.Channel)
		if err != nil {
			return nil, err
		}
		m, err := at(e).Parse()
		if err != nil {
			return nil, err
		}
		return wrap(team, conv, m), nil
	}
}

// A new message is the event itself; an edit nests the new version.
var (
	messageCreated = messageEvent(
		func(e Event) Message { return e.Message },
		func(t domain.TeamID, c domain.ConversationID, m domain.Message) domain.Event {
			return domain.MessageCreated{Team: t, Conversation: c, Message: m}
		})
	messageUpdated = messageEvent(
		func(e Event) Message { return e.Edit },
		func(t domain.TeamID, c domain.ConversationID, m domain.Message) domain.Event {
			return domain.MessageUpdated{Team: t, Conversation: c, Message: m}
		})
)

func messageDeleted(team domain.TeamID, e Event) (domain.Event, error) {
	conv, ts, err := ParseItem(e.Channel, e.DeletedTS)
	if err != nil {
		return nil, err
	}
	return domain.MessageDeleted{Team: team, Conversation: conv, TS: ts}, nil
}

// errNoEmoji reports a reaction event without its emoji name.
var errNoEmoji = errors.New("reaction without an emoji name")

// ErrUnsupportedReactionItem reports a reaction on something other than a
// message (a file, a file comment). The domain tracks reactions on
// messages only, so callers drop the event and log why.
var ErrUnsupportedReactionItem = errors.New("reaction on an item that is not a message")

func reactionChanged(added bool) Mapper {
	return func(team domain.TeamID, e Event) (domain.Event, error) {
		// The web client's socket sometimes sends no item type; slk takes
		// those as messages, and so does this mapper.
		if t := e.Item.Type; t != "message" && t != "" {
			return nil, fmt.Errorf("%w: %q", ErrUnsupportedReactionItem, t)
		}
		conv, ts, err := ParseItem(e.Item.Channel, e.Item.TS)
		if err != nil {
			return nil, err
		}
		user, err := domain.ParseUserID(e.User)
		if err != nil {
			return nil, err
		}
		if e.Reaction == "" {
			return nil, errNoEmoji
		}
		return domain.ReactionChanged{Team: team, Conversation: conv, TS: ts, User: user, Emoji: e.Reaction, Added: added}, nil
	}
}

// ParseItem parses the conversation and ts that point at one message.
func ParseItem(channel, ts string) (domain.ConversationID, domain.TS, error) {
	conv, err := domain.ParseConversationID(channel)
	if err != nil {
		return domain.ConversationID{}, domain.TS{}, err
	}
	t, err := domain.ParseTS(ts)
	return conv, t, err
}

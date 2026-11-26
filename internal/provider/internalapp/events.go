package internalapp

import (
	"encoding/json"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Events API → domain events.
//
// Mapped: message (no subtype, thread_broadcast, file_share, me_message) →
// MessageCreated; message_changed → MessageUpdated; message_deleted →
// MessageDeleted; reaction_added and reaction_removed on messages →
// ReactionChanged.
//
// Not available to an Events API app, so never emitted here:
//   - ReadChanged: the user's read marker events (channel_marked, im_marked,
//     group_marked) exist only in the RTM API. Read state reaches this
//     provider only through the Web API.
//   - UserTyping: user_typing is RTM-only too.
//   - PresenceChanged: presence_change is RTM-only; apps must poll
//     users.getPresence.
//
// Ignored: every other message subtype (channel_join, bot_message, …),
// reactions on files, and event types this package does not subscribe to.

// eventHead is what every event shares: enough to pick its mapper before
// decoding the fields, whose shapes differ between event types.
type eventHead struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

// callback is an Events API payload ("type": "event_callback"). The team
// in it is not read: events carry the provider's own team.
type callback[E any] struct {
	Event E `json:"event"`
}

// wireEvent holds the fields of every event type mapped here. The embedded
// wireMessage is the top-level message of a new-message event; its user
// is also the reacting user of a reaction event.
type wireEvent struct {
	eventHead
	wireMessage
	Channel   string      `json:"channel"`
	Message   wireMessage `json:"message"`
	DeletedTS string      `json:"deleted_ts"`
	Reaction  string      `json:"reaction"`
	Item      struct {
		Type    string `json:"type"`
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	} `json:"item"`
}

// eventMapper builds the domain event for one kind of wireEvent.
type eventMapper func(domain.TeamID, wireEvent) (domain.Event, error)

// eventMappers is keyed by wireEvent.kind.
var eventMappers = map[string]eventMapper{
	"message/":                 messageCreated,
	"message/thread_broadcast": messageCreated,
	"message/file_share":       messageCreated,
	"message/me_message":       messageCreated,
	"message/message_changed":  messageUpdated,
	"message/message_deleted":  messageDeleted,
	"reaction_added":           reactionChanged(true),
	"reaction_removed":         reactionChanged(false),
}

// kind is the event type, plus the subtype for messages.
func (e eventHead) kind() string {
	if e.Type == "message" {
		return "message/" + e.Subtype
	}
	return e.Type
}

// mapCallback maps one Events API payload to the domain event it means.
// It returns nil and no error for events the domain does not track, and an
// error for a malformed event, so no half-valid event is ever emitted.
func mapCallback(team domain.TeamID, payload []byte) (domain.Event, error) {
	var head callback[eventHead]
	if err := json.Unmarshal(payload, &head); err != nil {
		return nil, fmt.Errorf("decode events API payload: %w", err)
	}
	kind := head.Event.kind()
	mapEvent, ok := eventMappers[kind]
	if !ok {
		return nil, nil
	}
	var cb callback[wireEvent]
	if err := json.Unmarshal(payload, &cb); err != nil {
		return nil, fmt.Errorf("decode %s event: %w", kind, err)
	}
	ev, err := mapEvent(team, cb.Event)
	if err != nil {
		return nil, fmt.Errorf("%s event: %w", kind, err)
	}
	return ev, nil
}

func messageCreated(team domain.TeamID, e wireEvent) (domain.Event, error) {
	conv, m, err := parseChannelMessage(e.Channel, e.wireMessage)
	if err != nil {
		return nil, err
	}
	return domain.MessageCreated{Team: team, Conversation: conv, Message: m}, nil
}

func messageUpdated(team domain.TeamID, e wireEvent) (domain.Event, error) {
	conv, m, err := parseChannelMessage(e.Channel, e.Message)
	if err != nil {
		return nil, err
	}
	return domain.MessageUpdated{Team: team, Conversation: conv, Message: m}, nil
}

func messageDeleted(team domain.TeamID, e wireEvent) (domain.Event, error) {
	conv, ts, err := parseItem(e.Channel, e.DeletedTS)
	if err != nil {
		return nil, err
	}
	return domain.MessageDeleted{Team: team, Conversation: conv, TS: ts}, nil
}

// errNoEmoji reports a reaction event without its emoji name.
var errNoEmoji = errors.New("reaction without an emoji name")

func reactionChanged(added bool) eventMapper {
	return func(team domain.TeamID, e wireEvent) (domain.Event, error) {
		if e.Item.Type != "message" {
			return nil, nil
		}
		conv, ts, err := parseItem(e.Item.Channel, e.Item.TS)
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

func parseChannelMessage(channel string, w wireMessage) (domain.ConversationID, domain.Message, error) {
	conv, err := domain.ParseConversationID(channel)
	if err != nil {
		return domain.ConversationID{}, domain.Message{}, err
	}
	m, err := w.parse()
	return conv, m, err
}

// parseItem parses the conversation and ts that point at one message.
func parseItem(channel, ts string) (domain.ConversationID, domain.TS, error) {
	conv, err := domain.ParseConversationID(channel)
	if err != nil {
		return domain.ConversationID{}, domain.TS{}, err
	}
	t, err := domain.ParseTS(ts)
	return conv, t, err
}

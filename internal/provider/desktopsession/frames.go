// Adapted from github.com/gammons/slk internal/slack/events.go (MIT, Copyright (c) 2026 Grant Ammons).

package desktopsession

import (
	"encoding/json"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// Web client socket frames → domain events.
//
// Mapped:
//   - hello → ConnectionChanged{Connected}: the socket is live.
//   - messages and reactions, with slackwire's mappers (the frames have the
//     same shape as Events API events): message (no subtype,
//     thread_broadcast, file_share, me_message, bot_message) →
//     MessageCreated; message_changed → MessageUpdated; message_deleted →
//     MessageDeleted; reaction_added and reaction_removed on a message (or
//     on an item with no type) → ReactionChanged. Reactions on files are
//     logged and dropped.
//   - channel_marked, im_marked, group_marked, mpim_marked → ReadChanged:
//     the user's read marker moved, in any client.
//   - presence_change → one PresenceChanged per user. With
//     batch_presence_aware=1 the socket sends "users" instead of "user".
//   - user_typing → UserTyping.
//   - manual_presence_change → PresenceChanged with a zero User: the
//     signed-in user set their own presence, in any client. The provider
//     does not know who that is; the sync engine fills it in.
//   - dnd_updated → DNDChanged: the signed-in user's snooze changed, in
//     any client (its dnd_status carries the snooze; a schedule alone is
//     not one).
//
// Ignored: ephemeral messages (Slack never returns them from history, so
// a store must not keep them), every other message subtype,
// dnd_updated_user (other people's schedules, with no snooze in them), thread_marked
// (the domain has no per-thread read marker yet), reconnect_url (the sync
// engine reconnects with a fresh URL), goodbye (the server closes the
// socket right after it, which ends the stream), and every other frame.

// frame holds the fields of every frame mapped here: slackwire's, plus
// the ones only the web client's socket sends.
type frame struct {
	slackwire.Event
	Users     []string `json:"users"`
	Presence  string   `json:"presence"`
	Ephemeral bool     `json:"is_ephemeral"`
	// DND is set on a dnd_updated frame.
	DND *slackwire.DNDStatus `json:"dnd_status"`
	// Failure is set on an error frame: Slack's answer to start arguments
	// it rejects.
	Failure *frameError `json:"error"`
}

type frameError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// frameMapper builds the domain events of one kind of frame. It returns
// no events and no error for a frame the domain does not track.
type frameMapper func(domain.TeamID, frame) ([]domain.Event, error)

// frameMappers is keyed by slackwire.Head.Kind. Kinds missing here fall
// back to slackwire's mappers.
var frameMappers = map[string]frameMapper{
	"hello":           hello,
	"error":           errorFrame,
	"channel_marked":  readChanged,
	"im_marked":       readChanged,
	"group_marked":    readChanged,
	"mpim_marked":     readChanged,
	"presence_change": presenceChanged,
	"user_typing":     userTyping,

	"manual_presence_change": ownPresenceChanged,
	"dnd_updated":            dndChanged,
}

// mapFrame maps one socket frame to the domain events it means. It returns
// an error for a malformed frame, so no half-valid event is ever emitted.
func mapFrame(team domain.TeamID, data []byte) ([]domain.Event, error) {
	var head slackwire.Head
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("decode frame: %w", err)
	}
	kind := head.Kind()
	mapper, ok := mapperFor(kind)
	if !ok {
		return nil, nil
	}
	var f frame
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode %s frame: %w", kind, err)
	}
	if f.Ephemeral {
		return nil, nil
	}
	evs, err := mapper(team, f)
	if err != nil {
		return nil, fmt.Errorf("%s frame: %w", kind, err)
	}
	return evs, nil
}

// mapperFor finds the mapper of kind: this package's, or slackwire's
// adapted to a frame.
func mapperFor(kind string) (frameMapper, bool) {
	if m, ok := frameMappers[kind]; ok {
		return m, true
	}
	shared, ok := slackwire.MapperFor(kind)
	if !ok {
		return nil, false
	}
	return func(team domain.TeamID, f frame) ([]domain.Event, error) {
		ev, err := shared(team, f.Event)
		if ev == nil {
			return nil, err
		}
		return []domain.Event{ev}, nil
	}, true
}

func hello(team domain.TeamID, _ frame) ([]domain.Event, error) {
	return []domain.Event{domain.ConnectionChanged{Team: team, State: domain.Connected}}, nil
}

// errorFrame turns Slack's error frame into an error naming its code and
// message, so the caller logs why Slack refused the socket.
func errorFrame(_ domain.TeamID, f frame) ([]domain.Event, error) {
	if f.Failure == nil {
		return nil, errors.New("no details")
	}
	return nil, fmt.Errorf("code %d: %s", f.Failure.Code, f.Failure.Msg)
}

func readChanged(team domain.TeamID, f frame) ([]domain.Event, error) {
	conv, ts, err := slackwire.ParseItem(f.Channel, f.TS)
	if err != nil {
		return nil, err
	}
	return []domain.Event{domain.ReadChanged{Team: team, Conversation: conv, LastRead: ts}}, nil
}

func presenceChanged(team domain.TeamID, f frame) ([]domain.Event, error) {
	presence, err := slackwire.ParsePresence(f.Presence)
	if err != nil {
		return nil, err
	}
	ids := f.Users
	if len(ids) == 0 {
		ids = []string{f.User}
	}
	evs := make([]domain.Event, 0, len(ids))
	for _, id := range ids {
		user, err := domain.ParseUserID(id)
		if err != nil {
			return nil, err
		}
		evs = append(evs, domain.PresenceChanged{Team: team, User: user, Presence: presence})
	}
	return evs, nil
}

func userTyping(team domain.TeamID, f frame) ([]domain.Event, error) {
	conv, err := domain.ParseConversationID(f.Channel)
	if err != nil {
		return nil, err
	}
	user, err := domain.ParseUserID(f.User)
	if err != nil {
		return nil, err
	}
	return []domain.Event{domain.UserTyping{Team: team, Conversation: conv, User: user}}, nil
}

// ownPresenceChanged leaves User zero: the frame is about the signed-in
// user, whose ID the provider does not know.
func ownPresenceChanged(team domain.TeamID, f frame) ([]domain.Event, error) {
	presence, err := slackwire.ParsePresence(f.Presence)
	if err != nil {
		return nil, err
	}
	return []domain.Event{domain.PresenceChanged{Team: team, Presence: presence}}, nil
}

func dndChanged(team domain.TeamID, f frame) ([]domain.Event, error) {
	if f.DND == nil {
		return nil, errors.New("no dnd_status")
	}
	return []domain.Event{domain.DNDChanged{Team: team, Until: f.DND.Until()}}, nil
}

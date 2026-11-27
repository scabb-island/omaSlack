package internalapp

import (
	"encoding/json"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// Events API → domain events. The mapping is slackwire's (messages and
// reactions), shared with the desktop-session provider.
//
// Not available to an Events API app, so never emitted here:
//   - ReadChanged: the user's read marker events (channel_marked, im_marked,
//     group_marked) exist only in the RTM API. Read state reaches this
//     provider only through the Web API.
//   - UserTyping: user_typing is RTM-only too.
//   - PresenceChanged: presence_change and manual_presence_change are
//     RTM-only; apps must poll users.getPresence. The sync engine reads
//     the user's own presence that way (provider.PresenceReader) and
//     publishes the changes omaslack makes; other people's presence is
//     not shown for this provider.
//   - DNDChanged: dnd_updated is RTM-only, and the Events API's
//     dnd_updated_user carries the schedule but no snooze. The sync engine
//     reads the snooze with dnd.info on every connect and publishes the
//     changes omaslack makes; a snooze set in another client shows after
//     the next reconnect.
//
// Ignored: every event slackwire has no mapper for, and event types this
// package does not subscribe to. Reactions on files are logged and dropped.

// callback is an Events API payload ("type": "event_callback"). The team
// in it is not read: events carry the provider's own team.
type callback[E any] struct {
	Event E `json:"event"`
}

// mapCallback maps one Events API payload to the domain event it means.
// It returns nil and no error for events the domain does not track, and an
// error for a malformed event, so no half-valid event is ever emitted.
func mapCallback(team domain.TeamID, payload []byte) (domain.Event, error) {
	var head callback[slackwire.Head]
	if err := json.Unmarshal(payload, &head); err != nil {
		return nil, fmt.Errorf("decode events API payload: %w", err)
	}
	kind := head.Event.Kind()
	mapEvent, ok := slackwire.MapperFor(kind)
	if !ok {
		return nil, nil
	}
	var cb callback[slackwire.Event]
	if err := json.Unmarshal(payload, &cb); err != nil {
		return nil, fmt.Errorf("decode %s event: %w", kind, err)
	}
	ev, err := mapEvent(team, cb.Event)
	if err != nil {
		return nil, fmt.Errorf("%s event: %w", kind, err)
	}
	return ev, nil
}

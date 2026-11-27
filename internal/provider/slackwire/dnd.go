package slackwire

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// The signed-in user's own Do Not Disturb (provider.DNDController) and
// presence (provider.PresenceReader). Both providers call the same Web API
// methods with their own credentials.

// DNDStatus is the snooze part of a Do Not Disturb status, as dnd.info,
// dnd.setSnooze and the socket's dnd_updated event carry it. A schedule
// (dnd_enabled with its next start and end) is not a snooze and is left
// out: omaslack only snoozes and ends snoozes.
type DNDStatus struct {
	SnoozeEnabled bool  `json:"snooze_enabled"`
	SnoozeEnd     int64 `json:"snooze_endtime"`
}

// Until is when the snooze ends; zero when there is none.
func (s DNDStatus) Until() time.Time {
	if !s.SnoozeEnabled || s.SnoozeEnd <= 0 {
		return time.Time{}
	}
	return time.Unix(s.SnoozeEnd, 0)
}

type dndResponse struct {
	Envelope
	DNDStatus
}

// DND calls dnd.info for the signed-in user and returns when their snooze
// ends; zero when it is off.
func (a *WebAPI) DND(ctx context.Context) (time.Time, error) {
	var resp dndResponse
	if err := a.Call(ctx, "dnd.info", url.Values{}, &resp); err != nil {
		return time.Time{}, err
	}
	return resp.Until(), nil
}

// errSnoozeLength reports a snooze shorter than a minute.
var errSnoozeLength = errors.New("dnd.setSnooze: the snooze must last at least one minute")

// SnoozeDND calls dnd.setSnooze, which turns Do Not Disturb on for
// minutes (at least 1), or changes the length of the current snooze. It
// returns when the snooze ends: Slack's snooze_endtime, or minutes from
// now when the answer leaves it out.
func (a *WebAPI) SnoozeDND(ctx context.Context, minutes int) (time.Time, error) {
	if minutes < 1 {
		return time.Time{}, errSnoozeLength
	}
	var resp dndResponse
	if err := a.Call(ctx, "dnd.setSnooze", url.Values{"num_minutes": {strconv.Itoa(minutes)}}, &resp); err != nil {
		return time.Time{}, err
	}
	if until := resp.Until(); !until.IsZero() {
		return until, nil
	}
	return a.now().Add(time.Duration(minutes) * time.Minute).Truncate(time.Second), nil
}

// EndDND calls dnd.endSnooze. Ending a snooze that is not on is not an
// error.
func (a *WebAPI) EndDND(ctx context.Context) error {
	return a.callUnlessDone(ctx, "dnd.endSnooze", url.Values{}, "snooze_not_active")
}

// ParsePresence maps Slack's presence value to the domain's.
func ParsePresence(s string) (domain.Presence, error) {
	switch s {
	case "active":
		return domain.Active, nil
	case "away":
		return domain.Away, nil
	}
	return 0, fmt.Errorf("unknown presence %q", s)
}

type presenceResponse struct {
	Envelope
	Presence string `json:"presence"`
}

// Presence calls users.getPresence for the signed-in user: away when they
// set themselves away (or Slack considers them idle), active otherwise.
func (a *WebAPI) Presence(ctx context.Context) (domain.Presence, error) {
	var resp presenceResponse
	if err := a.Call(ctx, "users.getPresence", url.Values{}, &resp); err != nil {
		return 0, err
	}
	p, err := ParsePresence(resp.Presence)
	if err != nil {
		return 0, fmt.Errorf("users.getPresence: %w", err)
	}
	return p, nil
}

package app

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// The user's own Do Not Disturb and presence, and other people's presence
// in DMs (task 5.3). The daemon owns all of it: the TUI asks dnd.info and
// presence.get when it loads a workspace, follows dnd.changed and
// presence.changed, and changes them only through dnd.snooze, dnd.end and
// presence.set, whose effect comes back as those events.

// tomorrowHour is when ":dnd tomorrow" ends, local time: the same as the
// plugin's Quick Panel (plugin/lib/quickpanel.js TOMORROW_HOUR).
const tomorrowHour = 9

const dndUsage = "Usage: :dnd 30m|1h|2h|tomorrow|off"

// ownState is one workspace's own state, as the daemon last said.
type ownState struct {
	dnd      wire.DNDEvent
	presence map[string]string // user → "active" | "away"; the user's own under their ID or ""
}

// SetDND records a dnd.info answer or a dnd.changed event.
func (s *State) SetDND(ev wire.DNDEvent) {
	if w := s.ws[ev.Team]; w != nil {
		w.own.dnd = ev
	}
}

// DND is the workspace's snooze as last reported.
func (s *State) DND(team string) wire.DNDEvent {
	if w := s.ws[team]; w != nil {
		return w.own.dnd
	}
	return wire.DNDEvent{}
}

// SetPresence records a user's presence (presence.changed). user "" is
// the signed-in user (presence.get, which does not name them).
func (s *State) SetPresence(team, user, presence string) {
	w := s.ws[team]
	if w == nil {
		return
	}
	if user == w.info.Self {
		user = ""
	}
	if w.own.presence == nil {
		w.own.presence = map[string]string{}
	}
	w.own.presence[user] = presence
}

// Presence is a user's presence, "" when unknown. user "" is the
// signed-in user.
func (s *State) Presence(team, user string) string {
	w := s.ws[team]
	if w == nil {
		return ""
	}
	if user == w.info.Self {
		user = ""
	}
	return w.own.presence[user]
}

func (s *State) dndChanged(ev wire.DNDEvent, _ time.Time) { s.SetDND(ev) }

func (s *State) presenceChanged(ev wire.PresenceEvent, _ time.Time) {
	s.SetPresence(ev.Team, ev.User, ev.Presence)
}

// loadOwn asks the daemon for the workspace's snooze and own presence.
func (m *Model) loadOwn(team string) tea.Cmd {
	return tea.Batch(
		optionalCall(m, "dnd.info", map[string]any{"team": team}, func(m *Model, ev wire.DNDEvent) tea.Cmd {
			ev.Team = team
			m.st.SetDND(ev)
			return nil
		}),
		optionalCall(m, "presence.get", map[string]any{"team": team}, func(m *Model, r struct {
			Presence string `json:"presence"`
		}) tea.Cmd {
			m.st.SetPresence(team, "", r.Presence)
			return nil
		}),
	)
}

// setPresence runs :away (away) and :back (automatic) in the active
// workspace.
func (m *Model) setPresence(away bool) tea.Cmd {
	presence := "auto"
	if away {
		presence = "away"
	}
	return call[any](m.client, "presence.set", map[string]any{"team": m.team, "presence": presence}, nil)
}

// dnd runs ":dnd <length>|tomorrow|off" in the active workspace.
func (m *Model) dnd(arg string) tea.Cmd {
	arg = strings.TrimSpace(arg)
	if arg == "off" {
		return call[any](m.client, "dnd.end", map[string]any{"team": m.team}, nil)
	}
	now := m.now()
	if m.loc != nil {
		now = now.In(m.loc)
	}
	minutes, err := snoozeMinutes(arg, now)
	if err != nil {
		m.setError(dndUsage)
		return nil
	}
	return call[any](m.client, "dnd.snooze", map[string]any{"team": m.team, "minutes": minutes}, nil)
}

var errSnooze = errors.New("not a snooze length")

// snoozeMinutes is how long ":dnd arg" snoozes at now: a Go duration of
// whole minutes ("30m", "1h", "90m"), or "tomorrow", until tomorrowHour
// the next day in now's zone.
func snoozeMinutes(arg string, now time.Time) (int, error) {
	if arg == "tomorrow" {
		y, mo, d := now.Date()
		then := time.Date(y, mo, d+1, tomorrowHour, 0, 0, 0, now.Location())
		return int(math.Round(then.Sub(now).Minutes())), nil
	}
	d, err := time.ParseDuration(arg)
	if err != nil || d < time.Minute || d%time.Minute != 0 {
		return 0, errSnooze
	}
	return int(d / time.Minute), nil
}

// ownStatus is the status bar's part for the active workspace: DND with
// the time left while snoozed, and "away" when the user is.
func (m *Model) ownStatus() []string {
	var out []string
	if d := m.st.DND(m.team); d.Snoozed {
		if left := time.Unix(d.Until, 0).Sub(m.now()); left > 0 {
			out = append(out, lipgloss.NewStyle().Foreground(m.theme.Warning).Render("DND "+dndLeft(left)))
		}
	}
	if m.st.Presence(m.team, "") == "away" {
		out = append(out, m.muted("away"))
	}
	return out
}

// dndLeft is a snooze's time left, rounded up to the minute: "42m",
// "1h", "1h30m".
func dndLeft(d time.Duration) string {
	minutes := int((d + time.Minute - 1) / time.Minute)
	switch h, mm := minutes/60, minutes%60; {
	case h == 0:
		return fmt.Sprintf("%dm", mm)
	case mm == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh%dm", h, mm)
	}
}

// presenceDot is the mark before a DM's title: ● active, ○ away, a blank
// while the daemon has not said (internal-app never does).
func (m *Model) presenceDot(c wire.Conversation, style lipgloss.Style) string {
	switch m.st.Presence(m.team, c.User) {
	case "active":
		return style.Foreground(m.theme.Success).Render("●")
	case "away":
		return style.Foreground(m.theme.Muted).Render("○")
	}
	return style.Render(" ")
}

package app

import (
	"cmp"
	"slices"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Row is one sidebar line: a section header, or a conversation.
type Row struct {
	Header string
	Conv   wire.Conversation
	Title  string
}

// Sidebar lays a workspace's conversations out in sections:
//
//   - Unread: every conversation with unread messages or mentions, the
//     ones with the most mentions first, then the most recent. What needs
//     attention is on top, so j/k from the top reaches it first.
//   - Channels: the rest of the public (#) and private (~) channels, by
//     name.
//   - Direct messages: the rest of the DMs (@) and group DMs (&), by name.
//
// Empty sections are left out.
func (s *State) Sidebar(team string) []Row {
	var unread, channels, dms []Row
	for _, c := range s.Conversations(team) {
		r := Row{Conv: c, Title: s.Title(team, c)}
		switch {
		case c.Unread || c.Mentions > 0:
			unread = append(unread, r)
		case c.Kind == "im" || c.Kind == "mpim":
			dms = append(dms, r)
		default:
			channels = append(channels, r)
		}
	}
	slices.SortFunc(unread, byAttention)
	slices.SortFunc(channels, byTitle)
	slices.SortFunc(dms, byTitle)
	var out []Row
	for _, sec := range []struct {
		name string
		rows []Row
	}{{"Unread", unread}, {"Channels", channels}, {"Direct messages", dms}} {
		if len(sec.rows) > 0 {
			out = append(out, Row{Header: sec.name})
			out = append(out, sec.rows...)
		}
	}
	return out
}

func byAttention(a, b Row) int {
	return cmp.Or(
		cmp.Compare(b.Conv.Mentions, a.Conv.Mentions),
		compareTS(b.Conv.Latest, a.Conv.Latest),
		byTitle(a, b),
	)
}

func byTitle(a, b Row) int { return cmp.Compare(a.Title, b.Title) }

// Title is how the TUI names a conversation: #channel, ~private, @person
// for a DM (from users.info; the user or conversation ID until it
// answers) and &a, b, c for a group DM.
func (s *State) Title(team string, c wire.Conversation) string {
	switch c.Kind {
	case "im":
		return "@" + cmp.Or(s.UserName(team, c.User), c.User, c.ID)
	case "mpim":
		return "&" + cmp.Or(groupName(c.Name), c.ID)
	case "private":
		return "~" + cmp.Or(c.Name, c.ID)
	}
	return "#" + cmp.Or(c.Name, c.ID)
}

// groupName turns Slack's "mpdm-ana--bo--cy-1" into "ana, bo, cy".
func groupName(name string) string {
	name = strings.TrimPrefix(name, "mpdm-")
	if i := strings.LastIndex(name, "-"); i > 0 {
		name = name[:i]
	}
	return strings.ReplaceAll(name, "--", ", ")
}

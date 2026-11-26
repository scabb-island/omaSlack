package wire

import "gitlab.com/scabb-island/omaslack/internal/domain"

// Counts is what the bar shows for one workspace or for all of them:
// unread DMs (one-to-one and group), unread mentions, unread channels and
// unread threads.
type Counts struct {
	DMs      int `json:"dms"`
	Mentions int `json:"mentions"`
	Channels int `json:"channels"`
	Threads  int `json:"threads"`
}

func (c Counts) add(o Counts) Counts {
	return Counts{DMs: c.DMs + o.DMs, Mentions: c.Mentions + o.Mentions, Channels: c.Channels + o.Channels, Threads: c.Threads + o.Threads}
}

// Summary is the result of unread.summary and the payload of its event.
type Summary struct {
	Workspaces map[string]Counts `json:"workspaces"`
	Total      Counts            `json:"total"`
}

// Summarize counts the unread state of each workspace's conversation
// list. Threads stay zero until thread read markers are tracked
// (deferred in tasks.md).
func Summarize(lists map[domain.TeamID][]domain.Conversation) Summary {
	s := Summary{Workspaces: make(map[string]Counts, len(lists))}
	for team, list := range lists {
		c := count(list)
		s.Workspaces[team.String()] = c
		s.Total = s.Total.add(c)
	}
	return s
}

func count(list []domain.Conversation) Counts {
	var c Counts
	for _, conv := range list {
		c.Mentions += conv.Mentions
		if !conv.Unread() {
			continue
		}
		switch conv.Kind {
		case domain.DirectMessage, domain.GroupMessage:
			c.DMs++
		case domain.Channel, domain.PrivateChannel:
			c.Channels++
		}
	}
	return c
}

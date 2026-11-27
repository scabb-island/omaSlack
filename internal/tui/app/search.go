package app

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/tui/render"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// jumpPages is how many pages of history, at most, a jump to a search
// result loads to reach the message.
const jumpPages = 10

// startSearch opens the command line with "search " typed, so / then
// the query then Enter runs :search.
func (m *Model) startSearch() tea.Cmd {
	m.mode, m.cmdline = commandMode, "search "
	return nil
}

// search runs search.messages in the active workspace and lists what it
// finds. A provider without search answers -32002.
func (m *Model) search(query string) tea.Cmd {
	if query == "" {
		m.setError("Usage: :search <query>")
		return nil
	}
	team := m.team
	cmd := call(m.client, "search.messages", map[string]any{"team": team, "query": query}, func(m *Model, hits []wire.SearchHit) tea.Cmd {
		m.showHits(team, query, hits)
		return nil
	})
	return explain(cmd, rpc.CodeUnsupportedCapability, "Search is not available for this workspace.")
}

// explain turns cmd's failure with code into the error text given.
func explain(cmd tea.Cmd, code rpc.Code, text string) tea.Cmd {
	return func() tea.Msg {
		r := cmd().(resultMsg)
		var e *rpc.Error
		if errors.As(r.err, &e) && e.Code == code {
			r.err, r.apply = nil, func(m *Model) tea.Cmd { m.setError(text); return nil }
		}
		return r
	}
}

func (m *Model) showHits(team, query string, hits []wire.SearchHit) {
	if len(hits) == 0 {
		m.setInfo("No messages match " + query + ".")
		return
	}
	items := make([]pickItem, len(hits))
	for i, hit := range hits {
		items[i] = pickItem{lines: m.hitLines(team, hit), pick: func(m *Model) tea.Cmd { return m.jump(team, hit) }}
	}
	m.openPicker(&picker{
		title: fmt.Sprintf("Search: %s (%d)", query, len(hits)),
		items: func(*Model, string) []pickItem { return items },
	})
}

// hitLines are a search result as the list shows it: where and when,
// then the message's first line.
func (m *Model) hitLines(team string, hit wire.SearchHit) []string {
	dir := directory{st: m.st, team: team}
	where := "#" + hit.Conv
	if c, ok := m.st.Conversation(team, hit.Conv); ok {
		where = m.st.Title(team, c)
	}
	who := cmp.Or(dir.UserName(hit.Message.Author), hit.Message.Author)
	head := lipgloss.NewStyle().Foreground(m.theme.Accent).Render(where) + m.muted(" · "+who+" · "+m.when(hit.Message.TS))
	w := min(m.width, pickerWidth) - 6
	text, _, _ := strings.Cut(render.Text(hit.Message.Text, dir, render.Options{Theme: m.theme, Width: w, Now: m.now(), Loc: m.loc}), "\n")
	return []string{head, "  " + text}
}

// when is a message's date and time in the TUI's time zone.
func (m *Model) when(ts string) string {
	sec, _, _ := strings.Cut(ts, ".")
	n, _ := strconv.ParseInt(sec, 10, 64)
	loc := m.loc
	if loc == nil {
		loc = time.Local
	}
	return time.Unix(n, 0).In(loc).Format("2006-01-02 15:04")
}

// jump opens a search result's conversation positioned on the message.
//
// The conversation opens as usual (its newest messages), with the
// message's top-level message selected at once, so the view does not mark
// the conversation read while it is away from the newest. Then
// messages.history is asked for the messages before the one just after
// the hit (justAfter): the hit and older ones. The daemon answers from its
// cache, and loads one older page from Slack when the cache holds too
// few, so a hit older than the cache may take several calls; jump asks
// again, up to jumpPages times, until the hit arrives. A reply opens its
// thread too, with the reply selected. Messages between the hit's page and
// the newest ones are not loaded until asked for; G goes to the newest.
func (m *Model) jump(team string, hit wire.SearchHit) tea.Cmd {
	open := m.focusConversation(team, hit.Conv)
	root := cmp.Or(hit.Message.ThreadTS, hit.Message.TS)
	m.msgSel = root
	return tea.Batch(open, m.seek(team, hit, jumpPages))
}

// seek asks for the history up to hit until hit's top-level message
// arrives, pages more times at most.
func (m *Model) seek(team string, hit wire.SearchHit, pages int) tea.Cmd {
	root := cmp.Or(hit.Message.ThreadTS, hit.Message.TS)
	t := target{team, hit.Conv, ""}
	return call(m.client, "messages.history", t.params("before", justAfter(root)), func(m *Model, msgs []wire.Message) tea.Cmd {
		m.st.PutMessages(team, hit.Conv, msgs)
		switch {
		case slices.ContainsFunc(msgs, func(msg wire.Message) bool { return msg.TS == root }):
			return m.landed(team, hit)
		case len(msgs) == 0 && pages > 1:
			return m.seek(team, hit, pages-1)
		}
		m.setInfo("Could not reach that message in the history.")
		return nil
	})
}

// landed shows a reply's thread once its root is loaded, if the user is
// still on that conversation.
func (m *Model) landed(team string, hit wire.SearchHit) tea.Cmd {
	root := cmp.Or(hit.Message.ThreadTS, hit.Message.TS)
	if m.team != team || m.openConv() != hit.Conv || m.msgSel != root || root == hit.Message.TS {
		return nil
	}
	cmd := m.openThread()
	m.threadSel = hit.Message.TS
	return cmd
}

// justAfter is the Slack timestamp one microsecond after ts, so
// messages.history's before (exclusive) includes ts itself.
func justAfter(ts string) string {
	sec, micro, _ := strings.Cut(ts, ".")
	s, _ := strconv.ParseInt(sec, 10, 64)
	u, _ := strconv.ParseInt(micro, 10, 64)
	u++
	return fmt.Sprintf("%010d.%06d", s+u/1_000_000, u%1_000_000)
}

package app

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// finderEntry is one conversation the finder can open.
type finderEntry struct {
	team  string
	title string // as the sidebar shows it: #general, @Ana
	conv  wire.Conversation
}

// What needs attention ranks higher in the finder: an unread
// conversation gains finderUnread, one with mentions finderMentions more.
// Both are below the gap between two match kinds (see fuzzyScore), so a
// better match always wins and attention breaks ties among equal ones.
const (
	finderUnread   = 100
	finderMentions = 50
)

// rankConversations keeps the entries that match query and orders them,
// best first:
//
//   - A query starting with #, ~, @ or & keeps only that kind; the rest is
//     matched, fuzzily, against the name without its sign.
//   - Score: the match (fuzzyScore) plus the attention boost.
//   - Ties: the most recent message first, then by name, then in the
//     order given (workspaces in rail order).
func rankConversations(entries []finderEntry, query string) []finderEntry {
	sign, rest := splitSign(query)
	type scored struct {
		finderEntry
		score int
	}
	var out []scored
	for _, e := range entries {
		s, ok := fuzzyScore(e.title[1:], rest)
		if ok && (sign == "" || strings.HasPrefix(e.title, sign)) {
			out = append(out, scored{e, s + attention(e.conv)})
		}
	}
	slices.SortStableFunc(out, func(a, b scored) int {
		return cmp.Or(cmp.Compare(b.score, a.score), compareTS(b.conv.Latest, a.conv.Latest), cmp.Compare(a.title, b.title))
	})
	ranked := make([]finderEntry, len(out))
	for i, s := range out {
		ranked[i] = s.finderEntry
	}
	return ranked
}

// splitSign splits a conversation sign off the front of query.
func splitSign(query string) (sign, rest string) {
	if query != "" && strings.ContainsRune("#~@&", rune(query[0])) {
		return query[:1], query[1:]
	}
	return "", query
}

func attention(c wire.Conversation) int {
	boost := 0
	if c.Unread || c.Mentions > 0 {
		boost += finderUnread
	}
	if c.Mentions > 0 {
		boost += finderMentions
	}
	return boost
}

// openFinder opens the conversation finder over every workspace. The
// ones not loaded yet are loaded now; they join the list as they answer.
func (m *Model) openFinder() tea.Cmd {
	m.openPicker(&picker{title: "Find a conversation", filter: true, items: (*Model).finderItems})
	var cmds []tea.Cmd
	for _, team := range m.st.Teams() {
		if !m.st.Loaded(team) {
			cmds = append(cmds, m.loadWorkspace(team))
		}
	}
	return tea.Batch(cmds...)
}

// finderEntries are the conversations of every loaded workspace.
func (m *Model) finderEntries() []finderEntry {
	var out []finderEntry
	for _, team := range m.st.Teams() {
		for _, c := range m.st.Conversations(team) {
			out = append(out, finderEntry{team: team, title: m.st.Title(team, c), conv: c})
		}
	}
	return out
}

func (m *Model) finderItems(query string) []pickItem {
	ranked := rankConversations(m.finderEntries(), query)
	items := make([]pickItem, len(ranked))
	for i, e := range ranked {
		items[i] = pickItem{
			lines: []string{m.finderLine(e)},
			pick:  func(m *Model) tea.Cmd { return m.focusConversation(e.team, e.conv.ID) },
		}
	}
	return items
}

// finderLine is an entry as the sidebar shows it, its workspace on the
// right.
func (m *Model) finderLine(e finderEntry) string {
	style := lipgloss.NewStyle().Foreground(m.theme.Foreground)
	if e.conv.Unread {
		style = style.Foreground(m.theme.Unread).Bold(true)
	}
	line := style.Render(e.title)
	if e.conv.Mentions > 0 {
		line += lipgloss.NewStyle().Foreground(m.theme.Mention).Render(fmt.Sprintf(" (%d)", e.conv.Mentions))
	}
	ws := m.st.Workspace(e.team).Name
	w := min(m.width, pickerWidth) - 4
	return fit(line, max(w-ansi.StringWidth(ws)-1, 1)) + " " + m.muted(ws)
}

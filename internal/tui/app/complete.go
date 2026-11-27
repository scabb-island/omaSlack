package app

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"gitlab.com/scabb-island/omaslack/internal/tui/render"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// SearchDebounce is how long the people autocomplete waits after the last
// key before asking users.search, so typing "@ana" asks once, not three
// times.
const SearchDebounce = 150 * time.Millisecond

// composeState is what compose adds on top of the textarea: the
// autocomplete popup, the mention tokens (see suggest.go), the message
// being edited and a delete waiting for its confirmation.
type composeState struct {
	trig      trigger      // what the popup completes
	items     []suggestion // the popup; empty when closed
	sel       int
	dismissed *anchor // Esc closed the popup on the trigger here
	seq       int     // bumped on every new trigger: older users.search answers are stale

	tokens   map[string]string // display token → wire token
	edit     *msgRef           // the message compose edits; nil when writing a new one
	deleting *msgRef           // the message waiting for y/n
}

// anchor is where a trigger sits in compose: its row and its sign's
// column.
type anchor struct{ row, col int }

// msgRef names one message.
type msgRef struct{ team, conv, ts string }

func (r msgRef) params(extra ...any) map[string]any {
	return target{r.team, r.conv, ""}.params(append([]any{"ts", r.ts}, extra...)...)
}

// mentionTokens is the session's display → wire token map.
func (m *Model) mentionTokens() map[string]string {
	if m.cx.tokens == nil {
		m.cx.tokens = map[string]string{}
	}
	return m.cx.tokens
}

// popupOpen says the autocomplete popup shows.
func (m *Model) popupOpen() bool { return m.mode == insertMode && len(m.cx.items) > 0 }

// composeKey handles the INSERT keys that compose's extras take before
// the textarea: the popup's while it shows, then the edit's.
func (m *Model) composeKey(k tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.popupOpen() {
		if cmd, ok := m.popupKey(k); ok {
			return cmd, true
		}
	}
	return m.editKey(k)
}

// popupKey: Tab or Enter accepts, ↑/↓ (Ctrl+p/Ctrl+n) move, Esc closes.
func (m *Model) popupKey(k tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(k, m.keys.Accept):
		m.accept()
		return nil, true
	case key.Matches(k, m.keys.NextItem):
		m.cx.sel = (m.cx.sel + 1) % len(m.cx.items)
		return nil, true
	case key.Matches(k, m.keys.PrevItem):
		m.cx.sel = (m.cx.sel + len(m.cx.items) - 1) % len(m.cx.items)
		return nil, true
	case key.Matches(k, m.keys.Back):
		m.cx.dismissed = &anchor{m.compose.Line(), m.cx.trig.start}
		m.closePopup()
		return nil, true
	}
	return nil, false
}

// accept replaces the trigger word with the selected suggestion and a
// space, and records its wire token.
func (m *Model) accept() {
	s := m.cx.items[m.cx.sel]
	for col := m.compose.Column(); col > m.cx.trig.start; col = m.compose.Column() {
		m.compose, _ = m.compose.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		if m.compose.Column() >= col {
			break // cannot delete further; never loop
		}
	}
	m.compose.InsertString(s.text + " ")
	if s.wire != "" {
		m.mentionTokens()[s.text] = s.wire
	}
	m.closePopup()
}

func (m *Model) closePopup() {
	m.cx.items, m.cx.sel, m.cx.trig = nil, 0, trigger{}
	m.cx.seq++
}

// currentTrigger is the word being completed at compose's cursor.
func (m *Model) currentTrigger() (trigger, bool) {
	lines := strings.Split(m.compose.Value(), "\n")
	row := m.compose.Line()
	if row >= len(lines) {
		return trigger{}, false
	}
	line := []rune(lines[row])
	return findTrigger(line[:min(m.compose.Column(), len(line))])
}

// refreshCompletion follows compose after a key: it opens, updates or
// closes the popup for the word at the cursor.
func (m *Model) refreshCompletion() tea.Cmd {
	trig, ok := m.currentTrigger()
	at := anchor{m.compose.Line(), trig.start}
	if d := m.cx.dismissed; d != nil && (!ok || *d != at) {
		m.cx.dismissed = nil
	}
	if !ok || m.cx.dismissed != nil {
		m.closePopup()
		return nil
	}
	if trig == m.cx.trig {
		return nil
	}
	if trig.sign != m.cx.trig.sign {
		m.cx.items = nil // another kind of list: the old one would mislead
	}
	m.cx.trig = trig
	m.cx.seq++
	return m.suggest(trig)
}

// suggest fills the popup for trig: channels and emoji from what the TUI
// holds, people from users.search after SearchDebounce.
func (m *Model) suggest(trig trigger) tea.Cmd {
	team := m.target.team
	switch trig.sign {
	case '#':
		m.showItems(channelSuggestions(m.st.Conversations(team), trig.query))
	case ':':
		m.showItems(emojiSuggestions(render.StandardEmojiNames(), m.st.customEmojiNames(team), trig.query))
	case '@':
		return m.searchPeople(team, trig)
	}
	return nil
}

func (m *Model) showItems(items []suggestion) {
	m.cx.items = items
	m.cx.sel = min(m.cx.sel, max(len(items)-1, 0))
}

// searchPeople asks users.search for trig once the user stops typing for
// SearchDebounce. Each step checks the trigger generation, so a key typed
// meanwhile cancels the wait and an answer for an older prefix is
// dropped.
func (m *Model) searchPeople(team string, trig trigger) tea.Cmd {
	seq := m.cx.seq
	current := func(m *Model) bool { return m.cx.seq == seq }
	params := map[string]any{"team": team, "q": trig.query, "limit": maxSuggestions}
	return tea.Tick(SearchDebounce, func(time.Time) tea.Msg {
		return resultMsg{apply: func(m *Model) tea.Cmd {
			if !current(m) {
				return nil
			}
			return optionalCall(m, "users.search", params, func(m *Model, users []wire.User) tea.Cmd {
				m.st.PutUsers(team, users)
				if current(m) {
					m.showItems(peopleSuggestions(users, trig.query))
				}
				return nil
			})
		}}
	})
}

// customEmojiNames are a workspace's custom emoji, sorted.
func (s *State) customEmojiNames(team string) []string {
	if w := s.ws[team]; w != nil {
		return slices.Sorted(maps.Keys(w.emoji))
	}
	return nil
}

// Popup layout.
const (
	minPopupWidth = 24
	maxPopupWidth = 48
)

var popupTitles = map[rune]string{'@': "People", '#': "Channels", ':': "Emoji"}

// withPopup draws the autocomplete popup over the bottom of the panes,
// inside their border, right above compose.
func (m *Model) withPopup(panes string, w int) string {
	if !m.popupOpen() {
		return panes
	}
	rows := strings.Split(panes, "\n")
	pw := m.popupWidth(w - 2)
	box := strings.Split(m.popupView(pw), "\n")
	off := len(rows) - 1 - len(box)
	if off < 1 {
		return panes
	}
	for i, b := range box {
		row := rows[off+i]
		rows[off+i] = ansi.Truncate(row, 1, "") + b + ansi.TruncateLeft(row, 1+pw, "")
	}
	return strings.Join(rows, "\n")
}

func (m *Model) popupWidth(w int) int {
	pw := minPopupWidth
	for _, s := range m.cx.items {
		pw = max(pw, ansi.StringWidth(s.label)+4)
	}
	return min(pw, maxPopupWidth, w)
}

func (m *Model) popupView(w int) string {
	lines := make([]string, len(m.cx.items))
	for i, s := range m.cx.items {
		lines[i] = "  " + s.label
		if i == m.cx.sel {
			cursor := lipgloss.NewStyle().Foreground(m.theme.Accent).Render("▌")
			lines[i] = cursor + lipgloss.NewStyle().Background(m.theme.Selection).Render(fit(" "+s.label, w-3))
		}
	}
	title := fmt.Sprintf("%s · Tab picks", popupTitles[m.cx.trig.sign])
	return m.box(title, lines, w, len(lines)+2, true)
}

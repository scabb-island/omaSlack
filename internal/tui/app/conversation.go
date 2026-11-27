package app

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// TypingInterval is how often, at most, typing.send goes out while the
// user types.
const TypingInterval = 3 * time.Second

// openResult is conversations.open's answer.
type openResult struct {
	Messages []wire.Message `json:"messages"`
	Draft    string         `json:"draft"`
}

// draftResult is drafts.get's answer.
type draftResult struct {
	Text string `json:"text"`
}

// openConversation opens conv in the active workspace: the draft being
// written is saved, the previous conversation closed, and the daemon
// answers with the newest messages and the draft.
func (m *Model) openConversation(conv string) tea.Cmd {
	team, prev := m.team, m.openConv()
	cmds := []tea.Cmd{m.retarget(target{team, conv, ""})}
	if prev != "" && prev != conv {
		cmds = append(cmds, call[any](m.client, "conversations.close", target{team, prev, ""}.params(), nil))
	}
	m.open[team], m.sel[team] = conv, conv
	m.msgSel, m.thread, m.threadSel, m.focus = "", "", "", messagesPane
	t := target{team, conv, ""}
	cmds = append(cmds, call(m.client, "conversations.open", t.params(), func(m *Model, r openResult) tea.Cmd {
		m.st.PutMessages(team, conv, r.Messages)
		m.st.SetDraft(t, r.Draft)
		m.syncCompose()
		return nil
	}))
	return tea.Batch(cmds...)
}

// focusConversation shows conv of team, as ui.focus asks.
func (m *Model) focusConversation(team, conv string) tea.Cmd {
	i := slices.Index(m.st.Teams(), team)
	if i < 0 || conv == "" {
		return nil
	}
	return tea.Batch(m.switchWorkspace(i), m.openConversation(conv))
}

// switchWorkspace makes the i-th workspace (0-based) the active one.
func (m *Model) switchWorkspace(i int) tea.Cmd {
	teams := m.st.Teams()
	if i < 0 || i >= len(teams) {
		m.setError(fmt.Sprintf("There is no workspace %d.", i+1))
		return nil
	}
	if teams[i] == m.team {
		return nil
	}
	m.team = teams[i]
	m.msgSel, m.thread, m.threadSel, m.focus = "", "", "", messagesPane
	if m.openConv() == "" && m.sidebarVisible() {
		m.focus = sidebarPane
	}
	cmds := []tea.Cmd{m.retarget(target{m.team, m.openConv(), ""})}
	if !m.st.Loaded(m.team) {
		cmds = append(cmds, m.loadWorkspace(m.team))
	}
	return tea.Batch(cmds...)
}

// openThread opens the selected message's thread (the newest message's
// when none is selected) in the thread pane.
func (m *Model) openThread() tea.Cmd {
	msgs := m.messages()
	if len(msgs) == 0 {
		return nil
	}
	root := msgs[len(msgs)-1].TS
	if m.msgSel != "" {
		root = m.msgSel
	}
	m.thread, m.threadSel, m.focus = root, "", threadPane
	t := target{m.team, m.openConv(), root}
	return tea.Batch(
		call(m.client, "messages.replies", t.params(), func(m *Model, msgs []wire.Message) tea.Cmd {
			m.st.PutMessages(t.team, t.conv, msgs)
			return nil
		}),
		call(m.client, "drafts.get", t.params(), func(m *Model, d draftResult) tea.Cmd {
			m.st.SetDraft(t, d.Text)
			m.syncCompose()
			return nil
		}),
	)
}

// loadOlder asks for the page of history before the oldest message shown.
func (m *Model) loadOlder() tea.Cmd {
	msgs := m.messages()
	if len(msgs) == 0 {
		return nil
	}
	t := target{m.team, m.openConv(), ""}
	return call(m.client, "messages.history", t.params("before", msgs[0].TS), func(m *Model, older []wire.Message) tea.Cmd {
		m.st.PutMessages(t.team, t.conv, older)
		return nil
	})
}

// markRead marks the open conversation read up to its newest message
// while the view follows it (nothing selected), once per message. The
// daemon tells every client through read.changed and unread.summary.
func (m *Model) markRead() tea.Cmd {
	msgs := m.messages()
	if len(msgs) == 0 || m.msgSel != "" {
		return nil
	}
	k := convKey{m.team, m.openConv()}
	newest := msgs[len(msgs)-1].TS
	c, _ := m.st.Conversation(k.team, k.conv)
	if compareTS(newest, c.LastRead) <= 0 || compareTS(newest, m.marked[k]) <= 0 {
		return nil
	}
	m.marked[k] = newest
	return call[any](m.client, "conversations.markRead", target{k.team, k.conv, ""}.params("ts", newest), nil)
}

// retarget points compose at t, saving the draft it leaves.
func (m *Model) retarget(t target) tea.Cmd {
	if t == m.target {
		return nil
	}
	m.cancelEdit() // compose must hold the draft before it is saved
	save := m.saveDraft()
	m.target = t
	m.compose.SetValue(m.st.Draft(t))
	return save
}

// saveDraft stores what compose holds as its target's draft, when it
// changed. Drafts live in the daemon, so every client sees them.
func (m *Model) saveDraft() tea.Cmd {
	t, text := m.target, m.compose.Value()
	if t.conv == "" || text == m.st.Draft(t) {
		return nil
	}
	m.st.SetDraft(t, text)
	return call[any](m.client, "drafts.set", t.params("text", text), nil)
}

// syncCompose shows a draft another client changed, unless the user is
// typing here.
func (m *Model) syncCompose() {
	if m.mode != insertMode && m.compose.Value() != m.st.Draft(m.target) {
		m.compose.SetValue(m.st.Draft(m.target))
	}
}

// startInsert enters INSERT mode on the open conversation, or on the
// open thread when the thread pane has the focus.
func (m *Model) startInsert() tea.Cmd {
	conv := m.openConv()
	if conv == "" {
		m.setError("Open a conversation first (Enter).")
		return nil
	}
	t := target{m.team, conv, ""}
	if m.focus == threadPane {
		t.thread = m.thread
	}
	save := m.retarget(t)
	m.mode = insertMode
	return tea.Batch(save, m.compose.Focus())
}

// insertKey handles INSERT mode: Enter sends, Esc saves the draft and
// goes back to NORMAL, anything else edits.
func (m *Model) insertKey(k tea.KeyPressMsg) tea.Cmd {
	if cmd, done := m.composeKey(k); done {
		return cmd
	}
	switch {
	case key.Matches(k, m.keys.Back):
		m.mode = normalMode
		m.compose.Blur()
		return m.saveDraft()
	case key.Matches(k, m.keys.Send):
		return m.send()
	case key.Matches(k, m.keys.Newline):
		m.compose.InsertString("\n")
		return nil
	case key.Matches(k, m.keys.Attach):
		return m.startAttach()
	}
	before := m.compose.Value()
	var cmd tea.Cmd
	m.compose, cmd = m.compose.Update(k)
	if m.compose.Value() != before {
		cmd = tea.Batch(cmd, m.typing())
	}
	return tea.Batch(cmd, m.refreshCompletion())
}

// send sends what compose holds. Compose clears at once; a failed send
// puts the text back.
func (m *Model) send() tea.Cmd {
	text := m.compose.Value()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	t := m.target
	m.compose.Reset()
	m.st.SetDraft(t, "") // the daemon clears it on send
	sent := call(m.client, "messages.send", t.params("text", toWire(text, m.mentionTokens())), func(m *Model, msg wire.Message) tea.Cmd {
		m.st.PutMessages(t.team, t.conv, []wire.Message{msg})
		return nil
	})
	return withUndo(sent, func(m *Model) {
		if m.target == t && m.compose.Value() == "" {
			m.compose.SetValue(text)
		}
	})
}

// typing tells the daemon the user is typing, at most every
// TypingInterval.
func (m *Model) typing() tea.Cmd {
	now := m.now()
	if m.without["typing.send"] || now.Sub(m.lastTyping) < TypingInterval {
		return nil
	}
	m.lastTyping = now
	return call[any](m.client, "typing.send", target{m.target.team, m.target.conv, ""}.params(), nil)
}

func (m *Model) sidebarVisible() bool {
	switch m.sidebar {
	case sidebarShown:
		return true
	case sidebarHidden:
		return false
	}
	return m.width >= narrowWidth
}

func (m *Model) toggleSidebar() {
	if m.sidebarVisible() {
		m.sidebar = sidebarHidden
		m.fixFocus()
		m.layoutCompose()
		return
	}
	m.sidebar = sidebarShown
	m.layoutCompose()
}

func (m *Model) resizeSidebar(d int) {
	m.sidebarWidth = min(max(m.sidebarWidth+d, minSidebarWidth), maxSidebarWidth)
	m.layoutCompose()
}

func tsList(msgs []wire.Message) []string {
	out := make([]string, len(msgs))
	for i, msg := range msgs {
		out[i] = msg.TS
	}
	return out
}

// fixFocus moves the focus off a pane that is not shown.
func (m *Model) fixFocus() {
	if !slices.Contains(m.panes(), m.focus) {
		m.focus = messagesPane
	}
}

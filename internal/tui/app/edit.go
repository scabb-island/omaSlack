package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Editing, deleting and replying to the selected message (NORMAL: e, D,
// R). Edit and delete are for the user's own messages; while the daemon
// does not say who the user is (Directory.Self is ""), the TUI lets the
// action through and shows Slack's refusal if it was not theirs.

// selectedMessage is the message the focused pane selects: the selected
// one, or the newest while the view follows it.
func (m *Model) selectedMessage() (wire.Message, bool) {
	msgs, sel := m.messages(), m.msgSel
	switch m.focus {
	case threadPane:
		msgs, sel = m.replies(), m.threadSel
	case sidebarPane:
		return wire.Message{}, false
	}
	if len(msgs) == 0 {
		return wire.Message{}, false
	}
	for _, msg := range msgs {
		if msg.TS == sel {
			return msg, true
		}
	}
	return msgs[len(msgs)-1], true
}

// ownSelected is the selected message when the user may verb it.
func (m *Model) ownSelected(verb string) (msgRef, wire.Message, bool) {
	msg, ok := m.selectedMessage()
	if !ok {
		m.setError("Select a message to " + verb + " first.")
		return msgRef{}, msg, false
	}
	if self := (directory{st: m.st, team: m.team}).Self(); self != "" && msg.Author != self {
		m.setError("You can only " + verb + " your own messages.")
		return msgRef{}, msg, false
	}
	return msgRef{m.team, m.openConv(), msg.TS}, msg, true
}

// startEdit loads the selected message into compose, in INSERT mode, with
// its mentions shown by name.
func (m *Model) startEdit() tea.Cmd {
	ref, msg, ok := m.ownSelected("edit")
	if !ok {
		return nil
	}
	save := m.saveDraft()
	dir := directory{st: m.st, team: ref.team}
	m.cx.edit = &ref
	m.compose.SetValue(toDisplay(msg.Text, dir.UserName, dir.ConversationName, m.mentionTokens()))
	m.mode = insertMode
	return tea.Batch(save, m.compose.Focus())
}

// editKey: while editing, Enter saves the edit and Esc cancels it.
func (m *Model) editKey(k tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.cx.edit == nil {
		return nil, false
	}
	switch {
	case key.Matches(k, m.keys.Back):
		m.cancelEdit()
		return nil, true
	case key.Matches(k, m.keys.Send):
		return m.saveEdit(), true
	}
	return nil, false
}

// cancelEdit ends an edit, if one is going, and gives compose its draft
// back.
func (m *Model) cancelEdit() {
	if m.cx.edit == nil {
		return
	}
	m.cx.edit = nil
	m.mode = normalMode
	m.compose.Blur()
	m.compose.SetValue(m.st.Draft(m.target))
}

// saveEdit sends the edit with messages.edit. Compose goes back to the
// draft at once; a failed edit comes back to compose to try again.
func (m *Model) saveEdit() tea.Cmd {
	ref, text := *m.cx.edit, m.compose.Value()
	if strings.TrimSpace(text) == "" {
		m.setError("An edit cannot be empty; to delete the message, press D in NORMAL.")
		return nil
	}
	m.cancelEdit()
	edited := call(m.client, "messages.edit", ref.params("text", toWire(text, m.mentionTokens())), func(m *Model, msg wire.Message) tea.Cmd {
		m.st.PutMessages(ref.team, ref.conv, []wire.Message{msg})
		return nil
	})
	return withUndo(edited, func(m *Model) {
		if m.mode == normalMode && m.cx.edit == nil {
			m.cx.edit = &ref
			m.compose.SetValue(text)
			m.mode = insertMode
			m.compose.Focus()
		}
	})
}

// askDelete asks, in the status bar, to confirm deleting the selected
// message.
func (m *Model) askDelete() tea.Cmd {
	ref, _, ok := m.ownSelected("delete")
	if !ok {
		return nil
	}
	m.cx.deleting = &ref
	m.setInfo("Delete this message? y/n")
	return nil
}

// confirmDelete takes the answer: y deletes with messages.delete (the
// message leaves every client through message.deleted), any other key
// keeps it.
func (m *Model) confirmDelete(k tea.KeyPressMsg) tea.Cmd {
	ref := *m.cx.deleting
	m.cx.deleting = nil
	if k.Text != "y" && k.Text != "Y" {
		m.setInfo("Kept the message.")
		return nil
	}
	return call[any](m.client, "messages.delete", ref.params(), func(m *Model, _ any) tea.Cmd {
		m.setInfo("Message deleted.")
		return nil
	})
}

// reply writes in the selected message's thread: it opens the thread and
// enters INSERT on it. In the thread pane it writes in that thread.
func (m *Model) reply() tea.Cmd {
	switch m.focus {
	case threadPane:
		return m.startInsert()
	case messagesPane:
		if open := m.openThread(); open != nil {
			return tea.Batch(open, m.startInsert())
		}
	}
	m.setError("Select a message to reply to first.")
	return nil
}

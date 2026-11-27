package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// key routes a key press by mode. Help, when open, takes any key to close.
func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	m.status = ""
	if m.help {
		m.help = false
		return nil
	}
	if m.cx.deleting != nil {
		return m.confirmDelete(k)
	}
	switch m.mode {
	case insertMode:
		return m.insertKey(k)
	case commandMode:
		return m.commandKey(k)
	}
	for _, a := range m.actions {
		if key.Matches(k, a.keys) {
			return a.do(m, k)
		}
	}
	return nil
}

// normalActions binds the NORMAL mode keys to what they do.
func (m *Model) normalActions() []action {
	k := m.keys
	return []action{
		{k.Down, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.move(1) }},
		{k.Up, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.move(-1) }},
		{k.Top, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.move(-maxMove) }},
		{k.Bottom, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.move(maxMove) }},
		{k.Left, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.focusStep(-1, false) }},
		{k.Right, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.focusStep(1, false) }},
		{k.NextPane, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.focusStep(1, true) }},
		{k.PrevPane, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.focusStep(-1, true) }},
		{k.Open, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.enter() }},
		{k.Workspace, func(m *Model, k tea.KeyPressMsg) tea.Cmd { return m.switchWorkspace(int(k.Code - '1')) }},
		{k.Finder, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.setInfo("The finder is coming soon."); return nil }},
		{k.Sidebar, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.toggleSidebar(); return nil }},
		{k.Narrower, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.resizeSidebar(-sidebarStep); return nil }},
		{k.Wider, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.resizeSidebar(sidebarStep); return nil }},
		{k.Insert, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.startInsert() }},
		{k.Command, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.mode, m.cmdline = commandMode, ""; return nil }},
		{k.Back, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.back() }},
		{k.Help, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.help = true; return nil }},
		{k.Quit, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.quit() }},

		{k.Edit, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.startEdit() }},
		{k.Delete, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.askDelete() }},
		{k.Reply, func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.reply() }},
	}
}

// maxMove moves to either end of a list.
const maxMove = 1 << 30

// move moves the focused pane's selection by d.
func (m *Model) move(d int) tea.Cmd {
	switch m.focus {
	case sidebarPane:
		m.moveSidebar(d)
		return nil
	case threadPane:
		m.threadSel, _ = step(tsList(m.replies()), m.threadSel, d)
		return nil
	}
	var atTop bool
	m.msgSel, atTop = step(tsList(m.messages()), m.msgSel, d)
	if atTop {
		return m.loadOlder()
	}
	return m.markRead()
}

// step moves sel by d in list (oldest first). "" stands for the newest,
// and is what moving onto the newest gives back, so the view follows new
// messages again. atTop says d tried to go past the oldest.
func step(list []string, sel string, d int) (next string, atTop bool) {
	if len(list) == 0 {
		return "", d < 0
	}
	i := len(list) - 1
	for j, ts := range list {
		if ts == sel {
			i = j
		}
	}
	j := min(max(i+d, 0), len(list)-1)
	if j == len(list)-1 {
		return "", i+d < 0
	}
	return list[j], i+d < 0
}

func (m *Model) moveSidebar(d int) {
	ids := m.sidebarIDs()
	if len(ids) == 0 {
		return
	}
	i := 0
	for j, id := range ids {
		if id == m.sel[m.team] {
			i = j
		}
	}
	m.sel[m.team] = ids[min(max(i+d, 0), len(ids)-1)]
}

// sidebarIDs are the conversations of the active workspace in sidebar order.
func (m *Model) sidebarIDs() []string {
	var ids []string
	for _, r := range m.st.Sidebar(m.team) {
		if r.Header == "" {
			ids = append(ids, r.Conv.ID)
		}
	}
	return ids
}

// keepSelection points the sidebar selection at a conversation that
// exists: the open one, else the first.
func (m *Model) keepSelection() {
	ids := m.sidebarIDs()
	for _, id := range ids {
		if id == m.sel[m.team] {
			return
		}
	}
	m.sel[m.team] = m.openConv()
	if m.sel[m.team] == "" && len(ids) > 0 {
		m.sel[m.team] = ids[0]
	}
}

// panes are the panes that can take the focus now, left to right.
func (m *Model) panes() []pane {
	var out []pane
	if m.sidebarVisible() {
		out = append(out, sidebarPane)
	}
	out = append(out, messagesPane)
	if m.thread != "" {
		out = append(out, threadPane)
	}
	return out
}

// focusStep moves the focus d panes; wrap makes Tab go round.
func (m *Model) focusStep(d int, wrap bool) tea.Cmd {
	ps := m.panes()
	i := 0
	for j, p := range ps {
		if p == m.focus {
			i = j
		}
	}
	j := i + d
	if wrap {
		j = (j + len(ps)) % len(ps)
	}
	m.focus = ps[min(max(j, 0), len(ps)-1)]
	return nil
}

// enter opens the selected conversation, or the selected message's thread.
func (m *Model) enter() tea.Cmd {
	switch m.focus {
	case sidebarPane:
		if id := m.sel[m.team]; id != "" {
			return m.openConversation(id)
		}
	case messagesPane:
		return m.openThread()
	}
	return nil
}

// back closes the thread pane.
func (m *Model) back() tea.Cmd {
	if m.thread == "" {
		return nil
	}
	cmd := m.retarget(target{m.team, m.openConv(), ""})
	m.thread, m.threadSel = "", ""
	if m.focus == threadPane {
		m.focus = messagesPane
	}
	return cmd
}

func (m *Model) quit() tea.Cmd { return then(m.saveDraft(), tea.Quit) }

// commandKey edits and runs the COMMAND mode line.
func (m *Model) commandKey(k tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(k, m.keys.Back):
		m.mode = normalMode
	case k.Code == tea.KeyEnter:
		m.mode = normalMode
		return m.runCommand(m.cmdline)
	case k.Code == tea.KeyBackspace:
		if m.cmdline == "" {
			m.mode = normalMode
		}
		r := []rune(m.cmdline)
		m.cmdline = string(r[:max(len(r)-1, 0)])
	default:
		m.cmdline += k.Text
	}
	return nil
}

// runCommand runs a COMMAND mode line (without the colon).
func (m *Model) runCommand(line string) tea.Cmd {
	name, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	switch name {
	case "":
		return nil
	case "q", "quit":
		return m.quit()
	case "help":
		m.help = true
		return nil
	case "open":
		return m.openByName(strings.TrimSpace(arg))
	}
	m.setError("Not a command: " + name)
	return nil
}

// openByName opens the conversation whose title matches name, with or
// without its #, ~, @ or & sign: exactly if one does, else the first one,
// in sidebar order, that starts with it. Case does not matter.
func (m *Model) openByName(name string) tea.Cmd {
	want := strings.ToLower(strings.TrimLeft(name, "#~@&"))
	if want == "" {
		m.setError("Usage: :open <name>")
		return nil
	}
	var prefix string
	for _, r := range m.st.Sidebar(m.team) {
		title := strings.ToLower(strings.TrimLeft(r.Title, "#~@&"))
		if r.Header == "" && title == want {
			return m.openConversation(r.Conv.ID)
		}
		if r.Header == "" && prefix == "" && strings.HasPrefix(title, want) {
			prefix = r.Conv.ID
		}
	}
	if prefix != "" {
		return m.openConversation(prefix)
	}
	m.setError("No conversation called " + name)
	return nil
}

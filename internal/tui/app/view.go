package app

import (
	"cmp"
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"gitlab.com/scabb-island/omaslack/internal/tui/render"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Layout, in columns and rows.
const (
	railWidth           = 6  // workspace rail: " 1 Ac" and a badge
	defaultSidebarWidth = 28 // sidebar, border included
	minSidebarWidth     = 16
	maxSidebarWidth     = 48
	sidebarStep         = 4
	narrowWidth         = 80 // below it the sidebar hides unless asked for
	minThreadSplit      = 60 // main area width from which the thread pane sits beside the messages
	composeLines        = 3  // compose text rows (plus its border)
)

// View draws the whole screen:
//
//	rail │ sidebar │ messages │ thread
//	     │         │ compose          │
//	status bar (or the command line)
//
// The sidebar hides below narrowWidth columns (Ctrl+b shows it); the
// thread pane takes the messages' place when the main area is narrower
// than minThreadSplit.
func (m *Model) View() tea.View {
	v := tea.NewView(m.screen())
	v.AltScreen = true
	v.WindowTitle = "omaslack"
	return v
}

func (m *Model) screen() string {
	if m.width <= 0 || m.height <= 1 {
		return ""
	}
	h := m.height - 1
	body := m.body(h)
	if m.help {
		body = m.helpView(h)
	}
	return body + "\n" + m.statusBar()
}

func (m *Model) body(h int) string {
	cols := []string{m.rail(h)}
	if m.sidebarVisible() {
		cols = append(cols, m.sidebarView(m.sidebarWidth, h))
	}
	cols = append(cols, m.mainView(m.mainWidth(), h))
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

// mainWidth is what is left for messages, thread and compose.
func (m *Model) mainWidth() int {
	w := m.width - railWidth
	if m.sidebarVisible() {
		w -= m.sidebarWidth
	}
	return max(w, 4)
}

// layoutCompose fits compose to the main area.
func (m *Model) layoutCompose() {
	m.compose.SetWidth(max(m.mainWidth()-2, 1))
}

func (m *Model) mainView(w, h int) string {
	composeH := composeLines + 2
	top := max(h-composeH, 3)
	var panes string
	switch {
	case m.thread == "":
		panes = m.messagesView(w, top)
	case w >= minThreadSplit:
		mw := w / 2
		panes = lipgloss.JoinHorizontal(lipgloss.Top, m.messagesView(mw, top), m.threadView(w-mw, top))
	default:
		panes = m.threadView(w, top)
	}
	return panes + "\n" + m.composeView(w, composeH)
}

// rail lists the workspaces: their number (the 1–9 key), their initials
// and a badge, • for unread and ! for mentions.
func (m *Model) rail(h int) string {
	lines := make([]string, h)
	sum := m.st.Summary()
	for i, team := range m.st.Teams() {
		if i >= h {
			break
		}
		w := m.st.Workspace(team)
		line := fmt.Sprintf(" %d %-2s", i+1, initials(w.Name))
		style := lipgloss.NewStyle().Foreground(m.theme.Muted)
		if team == m.team {
			style = style.Foreground(m.theme.Accent).Background(m.theme.Selection).Bold(true)
		}
		lines[i] = style.Render(line) + m.badge(sum.Workspaces[team])
	}
	return m.column(lines, railWidth)
}

func (m *Model) badge(c wire.Counts) string {
	switch {
	case c.Mentions > 0:
		return lipgloss.NewStyle().Foreground(m.theme.Mention).Render("!")
	case c.DMs+c.Channels+c.Threads > 0:
		return lipgloss.NewStyle().Foreground(m.theme.Unread).Render("•")
	}
	return " "
}

func initials(name string) string {
	r := []rune(name)
	return string(r[:min(len(r), 2)])
}

// column pads lines to w columns and len(lines) rows.
func (m *Model) column(lines []string, w int) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fit(l, w)
	}
	return strings.Join(out, "\n")
}

// fit cuts or pads a styled line to exactly w columns.
func fit(line string, w int) string {
	line = ansi.Truncate(line, w, "…")
	return line + strings.Repeat(" ", max(w-ansi.StringWidth(line), 0))
}

// box draws lines in a rounded border w×h, the title in the top edge.
// The focused pane's border has the accent color.
func (m *Model) box(title string, lines []string, w, h int, focused bool) string {
	b := lipgloss.RoundedBorder()
	fg := m.theme.Border
	if focused {
		fg = m.theme.Accent
	}
	edge := lipgloss.NewStyle().Foreground(fg)
	iw, ih := max(w-2, 0), max(h-2, 0)
	if title != "" {
		title = " " + ansi.Truncate(title, max(iw-2, 0), "…") + " "
	}
	top := b.Top + title + strings.Repeat(b.Top, max(iw-1-ansi.StringWidth(title), 0))
	out := []string{edge.Render(b.TopLeft + ansi.Truncate(top, iw, "") + b.TopRight)}
	for i := range ih {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, edge.Render(b.Left)+fit(line, iw)+edge.Render(b.Right))
	}
	out = append(out, edge.Render(b.BottomLeft+strings.Repeat(b.Bottom, iw)+b.BottomRight))
	return strings.Join(out, "\n")
}

func (m *Model) muted(s string) string {
	return lipgloss.NewStyle().Foreground(m.theme.Muted).Render(s)
}

// sidebarView draws the active workspace's conversations, scrolled so the
// selection shows.
func (m *Model) sidebarView(w, h int) string {
	title := m.st.Workspace(m.team).Name
	ih := max(h-2, 0)
	var lines []string
	switch {
	case m.team == "":
		lines = []string{m.muted("No workspaces")}
	case !m.st.Loaded(m.team):
		lines = []string{m.muted("Loading…")}
	default:
		lines = m.sidebarLines()
	}
	return m.box(title, scrollTo(lines, m.sidebarSelLine(), ih), w, h, m.focus == sidebarPane)
}

func (m *Model) sidebarLines() []string {
	var lines []string
	for _, r := range m.st.Sidebar(m.team) {
		lines = append(lines, m.sidebarRow(r))
	}
	if len(lines) == 0 {
		lines = []string{m.muted("No conversations")}
	}
	return lines
}

func (m *Model) sidebarRow(r Row) string {
	if r.Header != "" {
		return lipgloss.NewStyle().Foreground(m.theme.Muted).Bold(true).Render(r.Header)
	}
	cursor, mark := " ", " "
	style := lipgloss.NewStyle().Foreground(m.theme.Foreground)
	if r.Conv.ID == m.sel[m.team] && m.focus == sidebarPane {
		cursor = lipgloss.NewStyle().Foreground(m.theme.Accent).Render("▌")
		style = style.Background(m.theme.Selection)
	}
	if r.Conv.ID == m.openConv() {
		mark = "▸"
	}
	if r.Conv.Unread {
		style = style.Foreground(m.theme.Unread).Bold(true)
	}
	line := cursor + style.Render(mark+" "+r.Title)
	if r.Conv.Mentions > 0 {
		line += lipgloss.NewStyle().Foreground(m.theme.Mention).Render(fmt.Sprintf(" (%d)", r.Conv.Mentions))
	}
	return line
}

// sidebarSelLine is the line of the selected conversation.
func (m *Model) sidebarSelLine() int {
	for i, r := range m.st.Sidebar(m.team) {
		if r.Header == "" && r.Conv.ID == m.sel[m.team] {
			return i
		}
	}
	return 0
}

// scrollTo returns at most h lines of lines, with line sel among them.
func scrollTo(lines []string, sel, h int) []string {
	start := max(sel-h+1, 0)
	return lines[start:min(len(lines), start+h)]
}

func (m *Model) messagesView(w, h int) string {
	var title string
	var lines []string
	switch {
	case m.team == "":
		lines = []string{"No workspaces yet. Sign in with omaslack auth add,", "then restart the daemon: systemctl --user restart omaslack"}
	case m.openConv() == "":
		lines = []string{m.muted("Pick a conversation and press Enter, or type :open <name>.")}
	default:
		c, _ := m.st.Conversation(m.team, m.openConv())
		title = m.st.Title(m.team, c)
		lines = m.messageLines(m.messages(), m.msgSel, w-2, h-2)
	}
	return m.box(title, lines, w, h, m.focus == messagesPane)
}

func (m *Model) threadView(w, h int) string {
	return m.box("Thread", m.messageLines(m.replies(), m.threadSel, w-2, h-2), w, h, m.focus == threadPane)
}

// messageLines renders msgs to fill h rows, bottom-aligned like a chat:
// the newest at the bottom, or the selected message at the bottom when
// one is selected. The last row says who is typing, when anyone is.
func (m *Model) messageLines(msgs []wire.Message, sel string, w, h int) []string {
	if typing := m.typingLine(); typing != "" {
		return append(m.messageTail(msgs, sel, w, h-1), typing)
	}
	return m.messageTail(msgs, sel, w, h)
}

func (m *Model) messageTail(msgs []wire.Message, sel string, w, h int) []string {
	if len(msgs) == 0 {
		return []string{m.muted("No messages yet.")}
	}
	selected := len(msgs) - 1
	for i, msg := range msgs {
		if msg.TS == sel {
			selected = i
		}
	}
	o := render.Options{Theme: m.theme, Width: w, Now: m.now(), Loc: m.loc}
	dir := directory{st: m.st, team: m.team}
	all := render.Messages(msgs, m.selectedIndex(sel, selected), dir, o)
	end := len(render.Messages(msgs[:selected+1], -1, dir, o))
	end = min(max(end, min(h, len(all))), len(all))
	lines := all[max(end-h, 0):end]
	return append(make([]string, max(h-len(lines), 0)), lines...)
}

// selectedIndex is the highlighted message: none while following the newest.
func (m *Model) selectedIndex(sel string, i int) int {
	if sel == "" {
		return -1
	}
	return i
}

func (m *Model) typingLine() string {
	conv := m.openConv()
	if conv == "" {
		return ""
	}
	var names []string
	for _, u := range m.st.Typing(m.team, conv, m.now()) {
		names = append(names, cmp.Or(m.st.UserName(m.team, u), u))
	}
	if len(names) == 0 {
		return ""
	}
	verb := " is typing…"
	if len(names) > 1 {
		verb = " are typing…"
	}
	return m.muted(strings.Join(names, ", ") + verb)
}

func (m *Model) composeView(w, h int) string {
	title := "Press i to write"
	if m.mode == insertMode {
		title = "Enter sends · Alt+Enter new line · Esc done"
	}
	if m.target.thread != "" {
		title = "Reply in thread · " + title
	}
	lines := strings.Split(m.compose.View(), "\n")
	return m.box(title, lines, w, h, m.mode == insertMode)
}

// statusBar shows the mode, the conversation, the workspace and its
// connection state, then the last message; in COMMAND mode, the command
// line.
func (m *Model) statusBar() string {
	if m.mode == commandMode {
		return fit(":"+m.cmdline+"█", m.width)
	}
	modeStyle := lipgloss.NewStyle().Bold(true).Foreground(m.theme.Background).Background(m.modeColor())
	parts := []string{modeStyle.Render(" " + m.mode.String() + " ")}
	if c, ok := m.st.Conversation(m.team, m.openConv()); ok {
		parts = append(parts, m.st.Title(m.team, c))
	}
	if m.team != "" {
		parts = append(parts, m.st.Workspace(m.team).Name, m.connection())
	}
	right := m.muted("? help")
	if m.status != "" {
		fg := m.theme.Foreground
		if m.statusIsError {
			fg = m.theme.Error
		}
		right = lipgloss.NewStyle().Foreground(fg).Render(m.status)
	}
	left := strings.Join(parts, m.muted(" │ "))
	gap := max(m.width-ansi.StringWidth(left)-ansi.StringWidth(right)-1, 1)
	return fit(left+strings.Repeat(" ", gap)+right, m.width)
}

func (m *Model) modeColor() color.Color {
	switch m.mode {
	case insertMode:
		return m.theme.Success
	case commandMode:
		return m.theme.Warning
	}
	return m.theme.Accent
}

// connection is the active workspace's connection state, colored; or that
// the daemon itself is gone.
func (m *Model) connection() string {
	state, fg := m.st.Workspace(m.team).State, m.theme.Warning
	switch {
	case m.gone:
		state, fg = "daemon gone", m.theme.Error
	case state == "connected":
		fg = m.theme.Success
	case state == "disconnected" || state == "auth_expired":
		fg = m.theme.Error
	}
	return lipgloss.NewStyle().Foreground(fg).Render("● " + state)
}

// helpView is the key map, generated from Keys, and the commands.
func (m *Model) helpView(h int) string {
	key := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true)
	var lines []string
	for _, g := range m.keys.groups() {
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render(g.title))
		for _, b := range g.keys {
			lines = append(lines, "  "+key.Render(fmt.Sprintf("%-12s", b.Help().Key))+b.Help().Desc)
		}
		lines = append(lines, "")
	}
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Commands (COMMAND)"))
	for _, c := range commandHelp {
		lines = append(lines, "  "+key.Render(fmt.Sprintf("%-14s", c[0]))+c[1])
	}
	lines = append(lines, "", m.muted("Press any key to close."))
	w := min(m.width, 64)
	box := m.box("Help", lines, w, min(h, len(lines)+2), true)
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, box)
}

// applyTheme styles compose from the theme; the panes read m.theme as
// they draw.
func (m *Model) applyTheme() {
	s := textarea.DefaultDarkStyles()
	text := lipgloss.NewStyle().Foreground(m.theme.Foreground)
	muted := lipgloss.NewStyle().Foreground(m.theme.Muted)
	for _, st := range []*textarea.StyleState{&s.Focused, &s.Blurred} {
		st.Base, st.Text, st.CursorLine = lipgloss.NewStyle(), text, text
		st.Placeholder, st.EndOfBuffer, st.Prompt = muted, muted, muted
	}
	s.Cursor.Color, s.Cursor.Blink = m.theme.Accent, false
	m.compose.SetStyles(s)
}

package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// pickState is the open list overlay (finder, search results, reaction
// picker), if any, and what lists remember between openings.
type pickState struct {
	open   *picker
	recent []string // emoji picked this session, most recent first
}

// picker is a list overlay: an optional filter line over items the user
// moves through and picks one of. Items are asked for on every key and
// frame, so they follow the State while it is open (a workspace that
// finishes loading, a name that arrives).
type picker struct {
	title  string
	filter bool // typing filters the list; without it, j/k move too
	query  string
	cursor int
	items  func(m *Model, query string) []pickItem
}

// pickItem is one entry: its lines (one or two, styled) and what picking
// it does.
type pickItem struct {
	lines []string
	pick  func(m *Model) tea.Cmd
}

// Overlay size: at most pickerWidth columns and pickerHeight rows.
const (
	pickerWidth  = 72
	pickerHeight = 24
)

func (m *Model) openPicker(p *picker) { m.pick.open = p }

// pickerKey handles a key while a list overlay is open: move, filter,
// pick (Enter) or close (Esc).
func (m *Model) pickerKey(k tea.KeyPressMsg) tea.Cmd {
	p := m.pick.open
	switch {
	case key.Matches(k, m.keys.Back):
		m.pick.open = nil
	case key.Matches(k, m.keys.Pick):
		return m.pickCurrent()
	case m.pickerMove(p, k):
	case p.filter:
		p.edit(k)
	}
	return nil
}

// pickerMove moves the cursor when k is a list movement key, and says
// whether it was. Without a filter, j/k move too.
func (m *Model) pickerMove(p *picker, k tea.KeyPressMsg) bool {
	switch {
	case key.Matches(k, m.keys.ListUp) || !p.filter && key.Matches(k, m.keys.Up):
		p.cursor = max(p.cursor-1, 0)
	case key.Matches(k, m.keys.ListDown) || !p.filter && key.Matches(k, m.keys.Down):
		p.cursor = min(p.cursor+1, max(len(p.items(m, p.query))-1, 0))
	default:
		return false
	}
	return true
}

// edit changes the filter: backspace deletes, text types. The cursor goes
// back to the best match.
func (p *picker) edit(k tea.KeyPressMsg) {
	r := []rune(p.query)
	switch {
	case k.Code == tea.KeyBackspace:
		p.query = string(r[:max(len(r)-1, 0)])
	case k.Text != "":
		p.query += k.Text
	default:
		return
	}
	p.cursor = 0
}

// pickCurrent closes the overlay and does what the item under the cursor
// does.
func (m *Model) pickCurrent() tea.Cmd {
	p := m.pick.open
	items := p.items(m, p.query)
	m.pick.open = nil
	if len(items) == 0 {
		return nil
	}
	return items[min(p.cursor, len(items)-1)].pick(m)
}

// pickerView draws the open list centered on an otherwise blank body.
func (m *Model) pickerView(h int) string {
	p := m.pick.open
	w, bh := min(m.width, pickerWidth), min(h, pickerHeight)
	iw := max(w-2, 1)
	var lines []string
	if p.filter {
		lines = append(lines, lipgloss.NewStyle().Foreground(m.theme.Accent).Render("> ")+p.query+"█", "")
	}
	listH := max(bh-2-len(lines)-2, 1)
	lines = append(lines, m.pickerLines(p, iw, listH)...)
	lines = append(lines, make([]string, max(bh-2-2-len(lines), 0))...)
	lines = append(lines, "", m.muted(m.pickerHint(p)))
	box := m.box(p.title, lines, w, bh, true)
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, box)
}

// pickerLines are the items' lines, the one under the cursor highlighted,
// scrolled so it shows in h rows.
func (m *Model) pickerLines(p *picker, w, h int) []string {
	items := p.items(m, p.query)
	sel := min(p.cursor, len(items)-1)
	if len(items) == 0 {
		return []string{m.muted("Nothing matches.")}
	}
	var lines []string
	selLine := 0
	for i, it := range items {
		for _, l := range it.lines {
			lines = append(lines, m.pickerLine(l, w, i == sel))
		}
		if i == sel {
			selLine = len(lines) - 1
		}
	}
	return scrollTo(lines, selLine, h)
}

func (m *Model) pickerLine(l string, w int, selected bool) string {
	if !selected {
		return " " + l
	}
	cursor := lipgloss.NewStyle().Foreground(m.theme.Accent).Render("▌")
	return cursor + lipgloss.NewStyle().Background(m.theme.Selection).Render(fit(l, w-1))
}

// pickerHint is the overlay's footer, from the key map.
func (m *Model) pickerHint(p *picker) string {
	move := m.keys.ListUp.Help().Key + " " + m.keys.ListDown.Help().Key
	if !p.filter {
		move = "j/k " + move
	}
	return strings.Join([]string{move + " move", m.keys.Pick.Help().Key + " " + m.keys.Pick.Help().Desc, "Esc close"}, " · ")
}

package app

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// helpColumn is the help overlay's width in one column. When one column
// does not fit the screen's height, the sections go in two, side by side,
// as wide as the screen.
const helpColumn = 62

// helpView is the key map, generated from Keys, and the commands.
func (m *Model) helpView(h int) string {
	sections := m.helpSections()
	footer := []string{"", m.muted("Press any key to close.")}
	var lines []string
	for _, s := range sections {
		lines = append(lines, s...)
	}
	w := min(m.width, helpColumn+2)
	if len(lines)+len(footer)+2 > h {
		lines, w = twoColumns(sections), m.width
	}
	lines = append(lines, footer...)
	box := m.box("Help", lines, w, min(h, len(lines)+2), true)
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, box)
}

// helpSections are the help's blocks: one per key group, then the
// commands. Each but the last ends with a blank line.
func (m *Model) helpSections() [][]string {
	key := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true)
	title := lipgloss.NewStyle().Bold(true)
	var out [][]string
	for _, g := range m.keys.groups() {
		s := []string{title.Render(g.title)}
		for _, b := range g.keys {
			s = append(s, "  "+key.Render(fmt.Sprintf("%-12s", b.Help().Key))+b.Help().Desc)
		}
		out = append(out, append(s, ""))
	}
	s := []string{title.Render("Commands (COMMAND)")}
	for _, c := range commandHelp {
		s = append(s, "  "+key.Render(fmt.Sprintf("%-16s", c[0]))+c[1])
	}
	return append(out, s)
}

// twoColumns lays whole sections out in two columns, the left one taking
// sections until it holds about half of the lines, and only as wide as
// its widest line, so the right one gets the rest.
func twoColumns(sections [][]string) []string {
	total := 0
	for _, s := range sections {
		total += len(s)
	}
	var left, right []string
	for _, s := range sections {
		if len(left) < total/2 && len(right) == 0 {
			left = append(left, s...)
			continue
		}
		right = append(right, s...)
	}
	leftW := 0
	for _, l := range left {
		leftW = max(leftW, ansi.StringWidth(l)+1)
	}
	out := make([]string, max(len(left), len(right)))
	for i := range out {
		out[i] = fit(at(left, i), leftW) + at(right, i)
	}
	return out
}

// at is lines[i], or "" past the end.
func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

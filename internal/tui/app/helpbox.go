package app

import "strings"

// helpWidth is the width of a one-column help box.
const helpWidth = 64

// helpBox boxes the help lines: in one column when they fit in h rows,
// else in two, split at the blank line nearest the middle, so no key goes
// missing as the key map grows.
func (m *Model) helpBox(lines []string, h int) string {
	if len(lines)+2 <= h || m.width < helpWidth+8 {
		return m.box("Help", lines, min(m.width, helpWidth), min(h, len(lines)+2), true)
	}
	w := min(m.width, 2*helpWidth)
	col := (w - 4) / 2
	left, right := halves(lines)
	rows := make([]string, max(len(left), len(right)))
	for i := range rows {
		rows[i] = fit(at(left, i), col) + "  " + at(right, i)
	}
	return m.box("Help", rows, w, min(h, len(rows)+2), true)
}

// halves splits lines at the blank line nearest the middle, which it
// drops; at the middle when there is none.
func halves(lines []string) (left, right []string) {
	mid := len(lines) / 2
	for d := range mid {
		for _, i := range []int{mid + d, mid - d} {
			if i > 0 && strings.TrimSpace(lines[i]) == "" {
				return lines[:i], lines[i+1:]
			}
		}
	}
	return lines[:mid], lines[mid:]
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

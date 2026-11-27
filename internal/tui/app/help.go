package app

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// helpGap is the space between help's two columns.
const helpGap = 3

// helpColumns lays help's lines out in two columns when one is too tall:
// it splits at the blank line (between two groups) nearest the middle, so
// no group is cut in two.
func helpColumns(lines []string) []string {
	cut := len(lines)
	for i, l := range lines {
		if l == "" && abs(2*(i+1)-len(lines)) < abs(2*cut-len(lines)) {
			cut = i + 1
		}
	}
	left, right := lines[:cut], lines[cut:]
	if n := len(left); n > 0 && left[n-1] == "" {
		left = left[:n-1]
	}
	lw := widest(left) + helpGap
	out := make([]string, max(len(left), len(right)))
	for i := range out {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = strings.TrimRight(fit(l, lw)+r, " ")
	}
	return out
}

func widest(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l))
	}
	return w
}

func abs(n int) int { return max(n, -n) }

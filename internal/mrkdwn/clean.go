package mrkdwn

import (
	"html"
	"strings"
	"unicode"
)

// Clean drops control characters (an ESC in a message must never reach a
// terminal) and expands tabs to four spaces, keeping newlines.
func Clean(s string) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// OneLine is Clean for what must stay on one line: names and text inside
// a line.
func OneLine(s string) string { return strings.ReplaceAll(Clean(s), "\n", " ") }

// unescape decodes HTML entities; &#27; must not smuggle an ESC in, nor
// &#10; a line break past a quote bar.
func unescape(s string) string { return OneLine(html.UnescapeString(s)) }

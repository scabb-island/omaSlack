package app

import "strings"

// Fuzzy match scores, best first. A subsequence scores below substring
// matches, less for every character it skips, and never below 1.
const (
	scoreExact     = 1000
	scorePrefix    = 800
	scoreWord      = 600 // the query starts a word: "dev" in "team-dev"
	scoreSubstring = 400
	scoreSubseq    = 200
)

// fuzzyScore says whether query matches name, ignoring case, and how
// well: exact, prefix, word start, substring, then the query's characters
// in order with gaps. An empty query matches everything with 0.
func fuzzyScore(name, query string) (int, bool) {
	name, query = strings.ToLower(name), strings.ToLower(query)
	switch i := strings.Index(name, query); {
	case query == "":
		return 0, true
	case name == query:
		return scoreExact, true
	case i == 0:
		return scorePrefix, true
	case i > 0 && isWordStart(name, i):
		return scoreWord, true
	case i > 0:
		return scoreSubstring, true
	}
	return subsequence(name, query)
}

// isWordStart says name[i] starts a word: it follows a separator.
func isWordStart(name string, i int) bool {
	return strings.ContainsRune(" -_.,", rune(name[i-1]))
}

// subsequence matches query's runes in order inside name, scoring
// scoreSubseq less one per rune skipped between the first and last match.
func subsequence(name, query string) (int, bool) {
	q := []rune(query)
	gaps, started := 0, false
	for _, r := range name {
		switch {
		case len(q) == 0:
		case r == q[0]:
			q, started = q[1:], true
		case started:
			gaps++
		}
	}
	return max(scoreSubseq-gaps, 1), len(q) == 0
}

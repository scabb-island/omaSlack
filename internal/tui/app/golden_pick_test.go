package app

import (
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/tui/client"
)

// Golden views of the list overlays (task 3.3): the finder, search
// results and the reaction picker. See golden_test.go.

func TestGoldenFinder(t *testing.T) {
	h := newHarness(t, daemon(nil), 120, 40)
	h.keys("ctrl+k")
	h.golden("finder_120x40")
	h.typeText("gnl")
	h.golden("finder_query_120x40")
}

func TestGoldenSearchResults(t *testing.T) {
	h := newHarness(t, daemon(map[string]client.Handler{"search.messages": client.Result(searchHits())}), 120, 40)
	h.keys("/")
	h.typeText("standup")
	h.keys("enter", "j")
	h.golden("search_120x40")
}

func TestGoldenReactionPicker(t *testing.T) {
	h := openGeneral(t, nil)
	h.keys(":")
	h.typeText("react smirk") // now a recent one: it comes first
	h.keys("enter", "r")
	h.typeText("smi")
	h.golden("react_120x40")
}

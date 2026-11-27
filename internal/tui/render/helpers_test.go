package render

import (
	"encoding/json"
	"os"
	"regexp"
	"sync"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/tui/theme"
)

// fakeDir is a Directory read from testdata/directory.json.
type fakeDir struct {
	SelfID        string            `json:"self"`
	Users         map[string]string `json:"users"`
	Conversations map[string]string `json:"conversations"`
	Custom        map[string]string `json:"emoji"`
}

func (f fakeDir) UserName(id string) string         { return f.Users[id] }
func (f fakeDir) ConversationName(id string) string { return f.Conversations[id] }
func (f fakeDir) Self() string                      { return f.SelfID }
func (f fakeDir) Emoji(name string) (string, bool) {
	s, ok := f.Custom[name]
	return s, ok
}

var testDir = sync.OnceValue(func() fakeDir {
	b, err := os.ReadFile("testdata/directory.json")
	if err != nil {
		panic(err)
	}
	var d fakeDir
	if err := json.Unmarshal(b, &d); err != nil {
		panic(err)
	}
	return d
})

// testLoc is fixed so goldens do not depend on the machine's zone or tzdata.
var testLoc = time.FixedZone("ART", -3*60*60)

func testOptions(width int) Options {
	return Options{
		Theme: theme.Default(),
		Width: width,
		Now:   time.Date(2026, 11, 26, 15, 0, 0, 0, testLoc),
		Loc:   testLoc,
	}
}

var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;:]*[A-Za-z]|\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)")

// strip removes the SGR and OSC sequences render emits, leaving the text a
// person would read.
func strip(s string) string { return ansiSeq.ReplaceAllString(s, "") }

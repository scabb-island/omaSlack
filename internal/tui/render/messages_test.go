package render

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// at is a Slack ts for a wall-clock time on the test clock's zone.
func at(day, hour, minute int) string {
	return fmt.Sprintf("%d.000100", time.Date(2026, 11, day, hour, minute, 0, 0, testLoc).Unix())
}

func readable(msgs []wire.Message, selected, width int) string {
	return strip(strings.Join(Messages(msgs, selected, testDir(), testOptions(width)), "\n"))
}

func TestMessagesLayout(t *testing.T) {
	msgs := []wire.Message{
		{TS: at(20, 9, 0), Author: "U0ALICE", Text: "last week"},
		{TS: at(25, 23, 58), Author: "U0ALICE", Text: "yesterday"},
		{TS: at(26, 0, 1), Author: "U0ALICE", Text: "after midnight"},
		{TS: at(26, 0, 4), Author: "U0ALICE", Text: "grouped", Edited: "1"},
		{TS: at(26, 0, 10), Author: "U0ALICE", Text: "six minutes later"},
		{TS: at(26, 0, 11), Author: "B0CI", Text: "build ok"},
		{TS: at(26, 0, 12), Author: "U0NEW", Text: ""},
	}
	want := strings.Join([]string{
		"── Friday, November 20 ─────────",
		"alice  09:00",
		"  last week",
		"── Yesterday ───────────────────",
		"alice  23:58",
		"  yesterday",
		"── Today ───────────────────────",
		"alice  00:01",
		"  after midnight",
		"  grouped (edited)",
		"alice  00:10",
		"  six minutes later",
		"ci-bot [bot]  00:11",
		"  build ok",
		"U0NEW  00:12",
	}, "\n")
	if got := readable(msgs, -1, 32); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A bot's message shows the name Slack put on it, which beats whatever
// the directory knows of its ID. One integration (one bot ID) posts under
// many names, so a new name starts a new group. Only bots carry a name.
func TestMessagesShowTheAuthorNameSlackSent(t *testing.T) {
	msgs := []wire.Message{
		{TS: at(26, 9, 0), Author: "B0HOOK", AuthorName: "deploybot", Text: "deploying"},
		{TS: at(26, 9, 1), Author: "B0HOOK", AuthorName: "deploybot", Text: "deployed"},
		{TS: at(26, 9, 2), Author: "B0HOOK", AuthorName: "alertbot", Text: "disk full"},
		{TS: at(26, 9, 3), Author: "B0CI", AuthorName: "GitLab CI", Text: "build ok"},
		// An app's bot user has a U… ID; the name still marks it a bot.
		{TS: at(26, 9, 4), Author: "U0GH", AuthorName: "GitHub", Text: "PR opened"},
	}
	want := strings.Join([]string{
		"── Today ───────────────────────",
		"deploybot [bot]  09:00",
		"  deploying",
		"  deployed",
		"alertbot [bot]  09:02",
		"  disk full",
		"GitLab CI [bot]  09:03",
		"  build ok",
		"GitHub [bot]  09:04",
		"  PR opened",
	}, "\n")
	if got := readable(msgs, -1, 32); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMessagesYearAndBadTS(t *testing.T) {
	msgs := []wire.Message{
		{TS: "1700000000.000100", Author: "U0BOB", Text: "old"},
		{TS: "garbage", Author: "U0BOB", Text: "bad ts"},
	}
	got := readable(msgs, -1, 40)
	for _, w := range []string{"Tuesday, November 14, 2023", "bob  19:13", "Wednesday, December 31, 1969", "bob  21:00"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
}

func TestMessagesAttachments(t *testing.T) {
	msgs := []wire.Message{{
		TS: at(26, 10, 0), ThreadTS: at(26, 10, 0), Author: "U0BOB", Text: "files", Edited: "1",
		Files: []wire.File{
			{ID: "F1", Name: "report.pdf", MIMEType: "application/pdf", Size: 1288490},
			{ID: "F2", Name: "cat.png", MIMEType: "image/png", Size: 348160},
			{ID: "F3", Title: "notes", Size: 12},
		},
		Reactions: []wire.Reaction{
			{Emoji: "+1", Users: []string{"U0ALICE", "U0SELF"}},
			{Emoji: "tada", Users: []string{"U0ALICE"}},
			{Emoji: "partyparrot", Users: []string{"U0ALICE"}},
			{Emoji: "nope", Users: []string{"U0ALICE"}},
		},
	}}
	want := strings.Join([]string{
		"── Today ─────────────────────────────────────────",
		"bob  10:00",
		"  files (edited)",
		"  [file] report.pdf · 1.2 MB · application/pdf",
		"  [image] cat.png · 340.0 KB · image/png",
		"  [file] notes · 12 B",
		"  👍 2  🎉 1  :partyparrot: 1  :nope: 1",
		"  ↳ thread",
	}, "\n")
	if got := readable(msgs, -1, 50); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A thread root says how many replies it has once the daemon knows.
func TestThreadMarkCountsReplies(t *testing.T) {
	for count, want := range map[int]string{0: "  ↳ thread", 1: "  ↳ 1 reply", 12: "  ↳ 12 replies"} {
		msgs := []wire.Message{{TS: at(26, 10, 0), ThreadTS: at(26, 10, 0), Author: "U0BOB", Text: "root", ReplyCount: count}}
		lines := strings.Split(readable(msgs, -1, 50), "\n")
		if got := lines[len(lines)-1]; got != want {
			t.Errorf("%d replies: %q, want %q", count, got, want)
		}
	}
}

func TestFileRowIsOneLine(t *testing.T) {
	msgs := []wire.Message{{TS: at(26, 10, 0), Author: "U0BOB", Files: []wire.File{
		{ID: "F2", Name: "a-very-long-file-name.png", MIMEType: "image/png", Size: 2048},
	}}}
	lines := Messages(msgs, -1, testDir(), testOptions(20))
	last := lines[len(lines)-1]
	if got := strip(last); got != "  [image] a-very-lo…" {
		t.Errorf("file row %q, want it cut to one line", got)
	}
	if _, ok := ImageFileID(last); !ok {
		t.Error("the cut row lost its image link")
	}
}

func TestReactionsHighlightSelf(t *testing.T) {
	msgs := []wire.Message{{TS: at(26, 10, 0), Author: "U0BOB", Text: "x", Reactions: []wire.Reaction{
		{Emoji: "+1", Users: []string{"U0SELF"}}, {Emoji: "tada", Users: []string{"U0BOB"}},
	}}}
	lines := Messages(msgs, -1, testDir(), testOptions(40))
	row := lines[len(lines)-1]
	mine, theirs, _ := strings.Cut(row, "🎉")
	if !strings.Contains(mine, "122;162;247") || strings.Contains(theirs, "122;162;247") {
		t.Errorf("only the user's own reaction should be in Theme.Accent: %q", row)
	}
}

func TestImageFileID(t *testing.T) {
	msgs := []wire.Message{{TS: at(26, 10, 0), Author: "U0BOB", Files: []wire.File{
		{ID: "F1", Name: "a.pdf", MIMEType: "application/pdf"}, {ID: "F2", Name: "cat.png", MIMEType: "image/png"},
	}}}
	var ids []string
	for _, l := range Messages(msgs, -1, testDir(), testOptions(40)) {
		if id, ok := ImageFileID(l); ok {
			ids = append(ids, id)
		}
	}
	if strings.Join(ids, ",") != "F2" {
		t.Errorf("image lines name %v, want [F2]", ids)
	}
}

func TestMessagesSelected(t *testing.T) {
	msgs := []wire.Message{
		{TS: at(26, 10, 0), Author: "U0BOB", Text: "first"},
		{TS: at(26, 10, 1), Author: "U0BOB", Text: "*second* is long enough to wrap", Reactions: []wire.Reaction{{Emoji: "+1"}}},
		{TS: at(26, 10, 2), Author: "U0BOB", Text: "third"},
	}
	lines := Messages(msgs, 1, testDir(), testOptions(20))
	const sel = "48;2;40;52;87"
	var marked []string
	for _, l := range lines {
		if strings.Contains(l, sel) {
			marked = append(marked, strip(l))
			if w := lipgloss.Width(l); w != 20 {
				t.Errorf("selected line %q is %d wide, want the full 20", strip(l), w)
			}
		}
	}
	if len(marked) != 3 || !strings.Contains(marked[0], "second") || !strings.Contains(marked[2], "👍") {
		t.Errorf("selected lines %q, want the second message's body and reactions", marked)
	}
}

// Names come from Slack, so they get the same cleaning as text, and a
// newline in one must not break the one-line header.
func TestDirectoryNamesStayOnOneLine(t *testing.T) {
	msgs := []wire.Message{{TS: at(26, 10, 0), Author: "U0EVIL", Text: "<@U0EVIL> in <#C0EVIL>"}}
	want := "── Today ─────────────────\nevil [31mname  10:00\n  @evil [31mname in #two\n  lines"
	if got := readable(msgs, -1, 26); got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestMessagesEmpty(t *testing.T) {
	if got := Messages(nil, -1, testDir(), testOptions(40)); len(got) != 0 {
		t.Errorf("got %q for no messages", got)
	}
}

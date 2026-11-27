package notify

import (
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

type testNames map[string]string

func (n testNames) UserName(id string) string         { return n["u:"+id] }
func (n testNames) ConversationName(id string) string { return n["c:"+id] }

func TestFormat(t *testing.T) {
	d := Details{
		Candidate: Candidate{
			Conversation: domain.Conversation{Kind: domain.Channel, Name: "general"},
			Message:      domain.Message{Text: "*hi* <@U0SELF> &amp; &lt;b&gt;"},
		},
		Author: "Alice",
		Names:  testNames{"u:U0SELF": "me"},
	}
	n := Format(d)
	if n.Summary != "Alice in #general" {
		t.Errorf("summary %q", n.Summary)
	}
	// Notification servers may read the body as markup, so it is escaped.
	if n.Body != "hi @me &amp; &lt;b&gt;" {
		t.Errorf("body %q", n.Body)
	}

	summaries := map[string]func(d *Details){
		"Alice":                      func(d *Details) { d.Conversation.Kind = domain.DirectMessage },
		"Alice in a group DM":        func(d *Details) { d.Conversation.Kind = domain.GroupMessage },
		"Alice in #general (thread)": func(d *Details) { d.Message.ThreadTS = ts(t, "1.000000"); d.Message.TS = ts(t, "2.000000") },
		"Someone in #general":        func(d *Details) { d.Author = "" },
		"Alice in #C0X":              func(d *Details) { d.Conversation.Name = ""; d.Conversation.ID = conv(t, "C0X") },
		"Alice in #general (Acme)":   func(d *Details) { d.Workspace = "Acme" },
	}
	for want, change := range summaries {
		d := d
		change(&d)
		if got := Format(d).Summary; got != want {
			t.Errorf("summary %q, want %q", got, want)
		}
	}
}

func TestFormatCleansAndShortens(t *testing.T) {
	d := Details{
		Candidate: Candidate{
			Conversation: domain.Conversation{Kind: domain.DirectMessage},
			Message:      domain.Message{Text: strings.Repeat("é", 400)}, // non-ASCII on purpose: cut on runes, not bytes
		},
		Author: "Bad\x1b[31m\nName",
	}
	n := Format(d)
	if n.Summary != "Bad[31m Name" {
		t.Errorf("summary %q", n.Summary)
	}
	if got := []rune(n.Body); len(got) != maxBody+1 || got[maxBody] != '…' {
		t.Errorf("body of %d runes", len(got))
	}
	d.Message.Text = ""
	d.Message.Files = []domain.File{{Name: "a.png"}}
	if n := Format(d); n.Body != "Sent a file" {
		t.Errorf("a message with only files: %q", n.Body)
	}
}

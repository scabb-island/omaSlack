package render

import (
	"strconv"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// BenchmarkMessages1000 renders a 1000-message conversation, the size the
// app keeps in view, mixing every kind of markup.
func BenchmarkMessages1000(b *testing.B) {
	texts := []string{
		"plain words that wrap across a line or two when the pane is narrow enough to need it",
		"*bold* _italic_ ~strike~ `code` <@U0ALICE> <@U0SELF> <#C0GEN> <https://example.com|link> :wave: :tada:",
		"&gt; a quote\n```\nfunc main() {}\n```\nafter",
		"日本語のテキスト 😀 ¡ñandú!", // intentional non-ASCII
	}
	authors := []string{"U0ALICE", "U0BOB", "B0CI"}
	msgs := make([]wire.Message, 1000)
	for i := range msgs {
		msgs[i] = wire.Message{
			TS:     strconv.Itoa(1795600000+i*120) + ".000100",
			Author: authors[i/3%len(authors)],
			Text:   texts[i%len(texts)],
		}
		if i%10 == 0 {
			msgs[i].Reactions = []wire.Reaction{{Emoji: "+1", Users: []string{"U0SELF", "U0BOB"}}}
			msgs[i].Files = []wire.File{{ID: "F1", Name: "cat.png", MIMEType: "image/png", Size: 4096}}
		}
	}
	d, o := testDir(), testOptions(80)
	b.ReportAllocs()
	for b.Loop() {
		Messages(msgs, 500, d, o)
	}
}

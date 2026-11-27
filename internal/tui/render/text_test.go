package render

import (
	"strings"
	"testing"
	"unicode"

	"charm.land/lipgloss/v2"
)

func TestTextReadable(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "hello world", "hello world"},
		{"bold italic strike", "*b* _i_ ~s~", "b i s"},
		{"nested", "*_both_*", "both"},
		{"snake case stays", "a_b_c and 2*3*4", "a_b_c and 2*3*4"},
		{"unclosed markers", "*open _half ~x", "*open _half ~x"},
		{"marker needs non-space", "* not bold *", "* not bold *"},
		{"code span keeps markers", "`*x* <@U0BOB>`", "*x* <@U0BOB>"},
		{"unclosed code", "a ` b", "a ` b"},
		{"entities", "a &amp; b &lt;c&gt;", "a & b <c>"},
		{"mention", "hi <@U0ALICE>", "hi @alice"},
		{"mention legacy label", "hi <@U0ZED|zed>", "hi @zed"},
		{"mention unknown", "hi <@U0ZED>", "hi @U0ZED"},
		{"channel label", "<#C0X|random>", "#random"},
		{"channel lookup", "<#C0GEN>", "#general"},
		{"channel unknown", "<#C0X>", "#C0X"},
		{"broadcasts", "<!here> <!channel> <!everyone>", "@here @channel @everyone"},
		{"subteam", "<!subteam^S1|@devs> <!subteam^S2>", "@devs @S2"},
		{"date fallback", "<!date^1700000000^{date}|Nov 14>", "Nov 14"},
		{"other special", "<!foo>", "@foo"},
		{"link label", "<https://x.y/a?b=1&amp;c=2|docs>", "docs"},
		{"bare link", "<https://x.y>", "https://x.y"},
		{"mailto", "<mailto:a@b.c>", "a@b.c"},
		{"empty angle", "a <> b", "a <> b"},
		{"unclosed angle", "a < b", "a < b"},
		{"empty target", "<|x>", "<|x>"},
		{"emoji", ":wave: hi :+1:", "👋 hi 👍"},
		{"skin tone", ":wave::skin-tone-3:", "👋🏼"},
		{"custom emoji", ":partyparrot:", ":partyparrot:"},
		{"unknown emoji", ":nope: 10:30:45", ":nope: 10:30:45"},
		{"quote", "&gt; quoted\nafter", "▎ quoted\nafter"},
		{"quote raw", "> raw", "▎ raw"},
		{"quote all", "before\n&gt;&gt;&gt; one\ntwo", "before\n▎ one\n▎ two"},
		{"code block", "see\n```\nfunc  x() {\n\treturn\n}\n```\ndone", "see\n│ func  x() {\n│     return\n│ }\ndone"},
		{"inline fence", "```a *b*```", "│ a *b*"},
		{"unclosed fence", "``` x", "``` x"},
		{"controls stripped", "a\x1b[31mb\x07c\r", "a[31mbc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strip(Text(c.in, testDir(), testOptions(80)))
			if got != c.want {
				t.Errorf("Text(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

func TestTextWraps(t *testing.T) {
	cases := []struct {
		name, in string
		width    int
		want     string
	}{
		{"words", "one two three four", 9, "one two\nthree\nfour"},
		{"wide runes", "日本語のテキスト", 6, "日本語\nのテキ\nスト"},
		{"emoji", "👋👋👋👋", 4, "👋👋\n👋👋"},
		{"quote", "&gt; aaa bbb ccc", 7, "▎ aaa\n▎ bbb\n▎ ccc"},
		{"code is cut not wrapped", "```abcdefghij```", 6, "│ abcd"},
		{"no width no wrap", "one two three", 0, "one two three"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strip(Text(c.in, testDir(), testOptions(c.width)))
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestTextStyles(t *testing.T) {
	o := testOptions(80)
	self := Text("<@U0SELF>", testDir(), o)
	other := Text("<@U0BOB>", testDir(), o)
	if self == other || !strings.Contains(self, "247;118;142") {
		t.Errorf("a mention of Self is not in Theme.Mention: %q", self)
	}
	if !strings.Contains(Text("<https://x.y|l>", testDir(), o), "\x1b]8;;https://x.y") {
		t.Error("links are not OSC 8 hyperlinks")
	}
	if strings.Contains(Text("<not a url|l>", testDir(), o), "\x1b]8;;") {
		t.Error("a target that is not a URL became a hyperlink")
	}
	if !strings.Contains(Text("&gt; q", testDir(), o), "187;154;247") {
		t.Error("the quote bar is not in Theme.Quote")
	}
	if !strings.Contains(Text("*b*", testDir(), o), "\x1b[1;") {
		t.Error("bold is not bold")
	}
}

func FuzzText(f *testing.F) {
	for _, s := range []string{
		"*a _b ~c~ d_ e*", "<@U0ALICE> <#C0GEN|g> <!here> <url|x>", ":wave::skin-tone-2:",
		"```x\n y```", "&gt;&gt;&gt; q\n> r", "&amp;&lt;&#x1F600;", "<<<>>>", "**__~~``",
		"日本語 😀 ññ", "\x1b]8;;evil\x07x", "a\tb", "<https://x\x1b[31m|y>",
	} {
		f.Add(s, 12)
	}
	f.Fuzz(func(t *testing.T, in string, width int) {
		width = 4 + abs(width)%80
		out := Text(in, testDir(), testOptions(width))
		for line := range strings.SplitSeq(strip(out), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("line %q is %d wide, more than %d", line, w, width)
			}
			if strings.ContainsFunc(line, unicode.IsControl) {
				t.Fatalf("line %q leaks a control character", line)
			}
		}
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

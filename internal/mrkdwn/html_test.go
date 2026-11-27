package mrkdwn

import (
	"regexp"
	"strings"
	"testing"
)

type names map[string]string

func (n names) UserName(id string) string         { return n["u:"+id] }
func (n names) ConversationName(id string) string { return n["c:"+id] }

var testNames = names{"u:U1": "alice", "u:U3": `<b>"bob"</b>`, "c:C1": "general"}

func TestHTML(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "hello", "hello"},
		{"escapes", `a < b & "c" 'd' &lt;e&gt; &amp;amp;`, "a &lt; b &amp; &#34;c&#34; &#39;d&#39; &lt;e&gt; &amp;amp;"},
		{"emphasis", "*b* _i_ ~s~ *_both_*", "<b>b</b> <i>i</i> <s>s</s> <b><i>both</i></b>"},
		{"code span", "`*x* &lt;y&gt;`", "<code>*x* &lt;y&gt;</code>"},
		{"lines", "one\ntwo\n\nthree", "one<br>two<br><br>three"},
		{"code block", "see\n```\nif a &lt; b {\n\tx()\n}\n```\ndone",
			"see<pre>if a &lt; b {\n    x()\n}</pre>done"},
		{"quote", "before\n&gt; q1\n&gt; q2\nafter", "before<blockquote>q1<br>q2</blockquote>after"},
		{"quote the rest", ">>> one\ntwo", "<blockquote>one<br>two</blockquote>"},
		{"user", "hi <@U1>", `hi <a href="omaslack://user/T1/U1">@alice</a>`},
		{"user legacy label", "<@U2|zed>", `<a href="omaslack://user/T1/U2">@zed</a>`},
		{"user unknown", "<@U2>", `<a href="omaslack://user/T1/U2">@U2</a>`},
		{"user name escaped", "<@U3>", `<a href="omaslack://user/T1/U3">@&lt;b&gt;&#34;bob&#34;&lt;/b&gt;</a>`},
		{"channel label", "<#C9|random>", `<a href="omaslack://conv/T1/C9">#random</a>`},
		{"channel lookup", "<#C1>", `<a href="omaslack://conv/T1/C1">#general</a>`},
		{"channel unknown", "<#C9>", `<a href="omaslack://conv/T1/C9">#C9</a>`},
		{"broadcasts", "<!here> <!channel> <!everyone>", "<b>@here</b> <b>@channel</b> <b>@everyone</b>"},
		{"subteam", "<!subteam^S1|@devs> <!subteam^S2>", "<b>@devs</b> <b>@S2</b>"},
		{"date", "<!date^1700000000^{date}|Nov 14> <!date^1>", "Nov 14 date^1"},
		{"other command", "<!foo>", "@foo"},
		{"link", "<https://x.y/a?b=1&amp;c=2|docs & more>", `<a href="https://x.y/a?b=1&amp;c=2">docs &amp; more</a>`},
		{"bare link", "<http://x.y>", `<a href="http://x.y">http://x.y</a>`},
		{"mailto", "<mailto:a@b.c>", `<a href="mailto:a@b.c">a@b.c</a>`},
		{"quote in url", `<https://x.y/"onclick=a|x>`, `<a href="https://x.y/&#34;onclick=a">x</a>`},
		{"unsafe scheme is text", "<javascript:alert(1)|click> <omaslack://user/T1/U1|me>", "click me"},
		{"url with space is text", "<https://x y|z>", "z"},
		{"emoji", ":wave: :+1::skin-tone-2: :partyparrot: 12:30", "👋 👍🏻 :partyparrot: 12:30"},
		{"malformed", "*open <> <|x> a < b ``` x", "*open &lt;&gt; &lt;|x&gt; a &lt; b ``` x"},
		{"controls dropped", "a\x1bb&#27;c", "abc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HTML(c.in, "T1", testNames); got != c.want {
				t.Errorf("HTML(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

func TestHTMLWithoutNames(t *testing.T) {
	if got := HTML("<@U1> <#C1>", "T1", nil); got != `<a href="omaslack://user/T1/U1">@U1</a> <a href="omaslack://conv/T1/C1">#C1</a>` {
		t.Errorf("got %q", got)
	}
}

// allowed matches every tag HTML may emit; anything else left after
// removing them is escaped text, so no '<' or '>' may remain.
var allowed = regexp.MustCompile(`</?(b|i|s|code|pre|blockquote)>|<br>|<a href="[^"<>]*">|</a>`)

func FuzzHTML(f *testing.F) {
	for _, s := range []string{
		"*b* _i_ `c` ```p``` > q", "<@U1|x\"y> <#C1|<b>> <https://a|<i>>", "<javascript:x|y>",
		"&lt;script&gt; &#60;b&#62; &quot;", ":wave: <!subteam^S1|@x> <!date^1|&lt;d>",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := allowed.ReplaceAllString(HTML(in, `T"1`, testNames), "")
		if strings.ContainsAny(out, "<>\"\x1b") {
			t.Fatalf("HTML(%q) leaves markup: %q", in, out)
		}
	})
}

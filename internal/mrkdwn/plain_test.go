package mrkdwn

import "testing"

func TestPlain(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "hello", "hello"},
		{"entities decoded, nothing escaped", "a &lt; b &amp; c", "a < b & c"},
		{"emphasis dropped", "*b* _i_ ~s~ *_both_*", "b i s both"},
		{"code span", "`*x*`", "*x*"},
		{"lines", "one\ntwo", "one\ntwo"},
		{"code block", "see\n```\nif a &lt; b {\n\tx()\n}\n```", "see\nif a < b {\n    x()\n}"},
		{"quote", "&gt; q1\nafter", "> q1\nafter"},
		{"user", "hi <@U1> <@U2|zed> <@U9>", "hi @alice @zed @U9"},
		{"channel", "<#C1> <#C9|random> <#C8>", "#general #random #C8"},
		{"broadcasts and groups", "<!here> <!subteam^S1|@devs> <!subteam^S2>", "@here @devs @S2"},
		{"date", "<!date^1700000000^{date}|Nov 14>", "Nov 14"},
		{"links", "<https://x.y|docs> <http://x.y> <mailto:a@b.c>", "docs http://x.y a@b.c"},
		{"emoji", "ship it :tada:", "ship it 🎉"},
		{"control characters dropped", "a\x1b[31mb", "a[31mb"},
	}
	for _, c := range cases {
		if got := Plain(c.in, testNames); got != c.want {
			t.Errorf("%s: Plain(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
	if got := Plain("<@U1>", nil); got != "@U1" {
		t.Errorf("no names: %q", got)
	}
}

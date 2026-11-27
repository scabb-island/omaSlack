package mrkdwn

import (
	"reflect"
	"testing"
)

func custom(name string) (string, bool) {
	if name == "partyparrot" {
		return ":partyparrot:", true
	}
	return "", false
}

func line(in ...Inline) Line { return Line{Inlines: in} }

func TestParse(t *testing.T) {
	cases := []struct {
		name, in string
		want     []Block
	}{
		{"plain", "a &amp; b", []Block{line(Text{"a & b"})}},
		{"emphasis nests", "*_x_* 2*3*4", []Block{line(
			Styled{Bold, []Inline{Styled{Italic, []Inline{Text{"x"}}}}}, Text{" 2*3*4"})}},
		{"strike", "~gone~", []Block{line(Styled{Strike, []Inline{Text{"gone"}}})}},
		{"snake case stays text", "a_b_c", []Block{line(Text{"a_b_c"})}},
		{"code span", "`*x* &lt;`", []Block{line(Code{"*x* <"})}},
		{"references", "<@U1> <@U2|zed> <#C1|gen> <!here> <!subteam^S1|@devs> <https://x.y/?a=1&amp;b=2|docs>",
			[]Block{line(User{ID: "U1"}, Text{" "}, User{ID: "U2", Label: "zed"}, Text{" "},
				Channel{ID: "C1", Label: "gen"}, Text{" "}, Special{Cmd: "here"}, Text{" "},
				Special{Cmd: "subteam^S1", Label: "@devs"}, Text{" "}, Link{URL: "https://x.y/?a=1&b=2", Label: "docs"})}},
		{"malformed references", "<> <|x> a < b", []Block{line(Text{"<> <|x> a < b"})}},
		{"emoji", ":wave::skin-tone-3: :partyparrot: :nope: 12:30", []Block{line(
			Shortcode{"wave", "👋"}, Shortcode{"skin-tone-3", "🏼"}, Text{" "}, Shortcode{"partyparrot", ":partyparrot:"}, Text{" :nope: 12:30"})}},
		{"quotes", "&gt; q\n>>> r\ns", []Block{
			Line{Quoted: true, Inlines: []Inline{Text{"q"}}},
			Line{Quoted: true, Inlines: []Inline{Text{"r"}}},
			Line{Quoted: true, Inlines: []Inline{Text{"s"}}}}},
		{"code block keeps entities and spaces", "a\n```\n\tx &lt;\n```\nb", []Block{
			line(Text{"a"}), CodeBlock{"    x &lt;"}, line(Text{"b"})}},
		{"unpaired fence is text", "``` x", []Block{line(Text{"``` x"})}},
		{"controls dropped", "a\x1bb&#27;c", []Block{line(Text{"abc"})}},
		{"empty line", "a\n\nb", []Block{line(Text{"a"}), line(), line(Text{"b"})}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Parse(c.in, custom); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Parse(%q)\n got %#v\nwant %#v", c.in, got, c.want)
			}
		})
	}
}

func TestParseWithoutCustomEmoji(t *testing.T) {
	want := []Block{line(Text{":partyparrot: "}, Shortcode{"tada", "🎉"})}
	if got := Parse(":partyparrot: :tada:", nil); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
}

func TestSpecial(t *testing.T) {
	s := Special{Cmd: "date^1700000000^{date}"}
	if s.Name() != "date" || s.Arg() != "1700000000^{date}" {
		t.Errorf("Name %q Arg %q", s.Name(), s.Arg())
	}
	if h := (Special{Cmd: "here"}); h.Name() != "here" || h.Arg() != "" {
		t.Errorf("Name %q Arg %q", h.Name(), h.Arg())
	}
}

func TestEmoji(t *testing.T) {
	if got, ok := Emoji("+1::skin-tone-2", nil); !ok || got != "👍🏻" {
		t.Errorf("reaction name: %q %v", got, ok)
	}
	if _, ok := Emoji("tada::nope", custom); ok {
		t.Error("an unknown part must make the whole name unknown")
	}
	if got, _ := Emoji("partyparrot", func(string) (string, bool) { return "a\nb\x1b", true }); got != "a b" {
		t.Errorf("custom text must be one clean line, got %q", got)
	}
}

func TestReferences(t *testing.T) {
	got := References("<@U1> *hi <@U2|x> <#C1>* <@U1> `<@U3>` <#C2|named> > <@W4> <#C1>\n```<@U5>```")
	want := Refs{Users: []string{"U1", "U2", "W4"}, Channels: []string{"C1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if got := References("no one"); !reflect.DeepEqual(got, Refs{}) {
		t.Errorf("got %v", got)
	}
}

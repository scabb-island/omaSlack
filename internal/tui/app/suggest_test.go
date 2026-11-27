package app

import (
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/wire"
)

func TestFindTrigger(t *testing.T) {
	for _, c := range []struct {
		before string
		want   trigger
		ok     bool
	}{
		{"@an", trigger{'@', "an", 0}, true},
		{"hi @an", trigger{'@', "an", 3}, true},
		{"hi (@an", trigger{'@', "an", 4}, true},
		{"@", trigger{}, false}, // users.search needs a letter
		{"mail me@an", trigger{}, false},
		{"#", trigger{'#', "", 0}, true},
		{"see #gen", trigger{'#', "gen", 4}, true},
		{"x#gen", trigger{}, false},
		{":t", trigger{}, false}, // emoji wait for two letters
		{"great :ta", trigger{':', "ta", 6}, true},
		{"great :+1", trigger{':', "+1", 6}, true},
		{"at 10:30", trigger{}, false},
		{"@ana ", trigger{}, false},
		{"plain", trigger{}, false},
		{"", trigger{}, false},
		// Non-ASCII on purpose: names are not only ASCII.
		{"@josé", trigger{'@', "josé", 0}, true},
	} {
		got, ok := findTrigger([]rune(c.before))
		if ok != c.ok || got != c.want {
			t.Errorf("findTrigger(%q) = %+v, %v; want %+v, %v", c.before, got, ok, c.want, c.ok)
		}
	}
}

func TestRankPrefersExactThenPrefixThenWords(t *testing.T) {
	names := []string{"random", "dev-general", "general-chat", "general", "regen"}
	got := rank(names, "gen")
	want := []string{"general", "general-chat", "dev-general", "regen"}
	var picked []string
	for _, i := range got {
		picked = append(picked, names[i])
	}
	if !reflect.DeepEqual(picked, want) {
		t.Errorf("rank = %v, want %v", picked, want)
	}
}

func TestRankIgnoresCase(t *testing.T) {
	if got := rank([]string{"Ana"}, "aN"); len(got) != 1 {
		t.Errorf("rank = %v", got)
	}
}

func TestPeopleSuggestions(t *testing.T) {
	users := []wire.User{
		{ID: "U3", Name: "zed", RealName: "Ana Zed"},
		{ID: "U1", Name: "ana", DisplayName: "Ana", RealName: "Ana Pérez"},
		{ID: "U9", Name: "x", RealName: "Matched by email"},
	}
	got := peopleSuggestions(users, "an")
	want := []suggestion{
		{label: "Ana · Ana Pérez", text: "@Ana", wire: "<@U1>"},
		{label: "Ana Zed · zed", text: "@Ana Zed", wire: "<@U3>"},
		{label: "Matched by email · x", text: "@Matched by email", wire: "<@U9>"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestPeopleSuggestionsOfferSpecialMentions(t *testing.T) {
	got := peopleSuggestions(nil, "he")
	want := []suggestion{{label: "@here · notify the active members", text: "@here", wire: "<!here>"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

func TestChannelSuggestions(t *testing.T) {
	convs := []wire.Conversation{
		{ID: "C2", Kind: "channel", Name: "random"},
		{ID: "C1", Kind: "channel", Name: "general"},
		{ID: "G1", Kind: "private", Name: "ops-gen"},
		{ID: "D1", Kind: "im", User: "U1"},
	}
	got := channelSuggestions(convs, "gen")
	want := []suggestion{
		{label: "#general", text: "#general", wire: "<#C1>"},
		{label: "#ops-gen", text: "#ops-gen", wire: "<#G1>"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	if all := channelSuggestions(convs, ""); len(all) != 3 {
		t.Errorf("an empty query lists every channel: %+v", all)
	}
}

func TestEmojiSuggestions(t *testing.T) {
	got := emojiSuggestions([]string{"tada", "smile", "potato"}, []string{"taco"}, "ta")
	if len(got) != 3 || got[0].text != ":taco:" || got[1].text != ":tada:" || got[2].text != ":potato:" {
		t.Errorf("got %+v", got)
	}
	if got[1].label != "\U0001F389 :tada:" {
		t.Errorf("a standard emoji shows itself: %q", got[1].label)
	}
	if got[0].label != ":taco:" {
		t.Errorf("a custom emoji shows its name only: %q", got[0].label)
	}
}

func TestSuggestionsAreCapped(t *testing.T) {
	names := make([]string, 30)
	for i := range names {
		names[i] = "a" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if got := emojiSuggestions(names, nil, "aa"); len(got) > maxSuggestions {
		t.Errorf("%d suggestions", len(got))
	}
}

func TestToWire(t *testing.T) {
	tokens := map[string]string{"@Ana": "<@U1>", "@Ana Zed": "<@U3>", "#general": "<#C1>", "@here": "<!here>"}
	for _, c := range []struct{ in, want string }{
		{"hi @Ana", "hi <@U1>"},
		{"@Ana, @Ana Zed.", "<@U1>, <@U3>."},
		{"@Anabel and @Ana_x", "@Anabel and @Ana_x"},
		{"see #general\n@here", "see <#C1>\n<!here>"},
		{"no tokens", "no tokens"},
	} {
		if got := toWire(c.in, tokens); got != c.want {
			t.Errorf("toWire(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToDisplay(t *testing.T) {
	names := map[string]string{"U1": "Ana"}
	convs := map[string]string{"C1": "general"}
	tokens := map[string]string{}
	got := toDisplay("<@U1> see <#C1> and <#C2|random>, <@U7>, <!here> &lt;b&gt; &amp;",
		func(id string) string { return names[id] }, func(id string) string { return convs[id] }, tokens)
	want := "@Ana see #general and #random, <@U7>, @here <b> &"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	wantTokens := map[string]string{"@Ana": "<@U1>", "#general": "<#C1>", "#random": "<#C2>", "@here": "<!here>"}
	if !reflect.DeepEqual(tokens, wantTokens) {
		t.Errorf("tokens = %v", tokens)
	}
	if back := toWire(got, tokens); back != "<@U1> see <#C1> and <#C2>, <@U7>, <!here> <b> &" {
		t.Errorf("round trip: %q", back)
	}
}

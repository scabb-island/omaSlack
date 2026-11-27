package app

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"gitlab.com/scabb-island/omaslack/internal/tui/render"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Compose autocomplete, the pure part: what is being completed, what to
// offer, and how the compose text becomes Slack's wire syntax.
//
// Mention tokens. Compose shows people and channels the way people read
// them ("@Ana", "#general") and sends them the way Slack wants them
// ("<@U1>", "<#C1>"). Accepting a suggestion records its display token
// and wire token in a map (completion.tokens); send runs toWire, which
// swaps every display token that stands as a whole word for its wire
// token. The textarea stays plain text, with no hidden markup to keep in
// step with the cursor. The map lives for the session, across
// conversations; a token typed by hand that matches one picked earlier
// is sent as that mention too, which is what the user means. Drafts keep
// the display text, so a draft restored after a restart sends "@Ana" as
// typed. Editing a message does the reverse with toDisplay, which fills
// the map from the message's own mentions.

// maxSuggestions is how many suggestions the popup shows.
const maxSuggestions = 8

// trigger is the word being completed: its sign ('@' people, '#'
// channels, ':' emoji), what follows the sign, and the sign's column on
// the cursor's line, in runes.
type trigger struct {
	sign  rune
	query string
	start int
}

// minQuery is how much a sign needs typed after it before suggesting:
// users.search needs a letter, and two letters keep ":" in "10:30" or a
// smiley from opening the emoji list.
var minQuery = map[rune]int{'@': 1, '#': 0, ':': 2}

// findTrigger finds the word being completed in before, the cursor's
// line up to the cursor. The sign must start a word: "me@an" is not a
// mention.
func findTrigger(before []rune) (trigger, bool) {
	i := len(before)
	for i > 0 && isNameRune(before[i-1]) {
		i--
	}
	if i == 0 {
		return trigger{}, false
	}
	start, sign := i-1, before[i-1]
	need, ok := minQuery[sign]
	query := string(before[i:])
	if !ok || utf8.RuneCountInString(query) < need || (start > 0 && !opensWord(before[start-1])) {
		return trigger{}, false
	}
	return trigger{sign, query, start}, true
}

// isNameRune says r can be part of a name, a channel or an emoji code.
func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-.+'", r)
}

func opensWord(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("([{\"'", r) }

// suggestion is one popup line: what it shows, what compose gets, and
// the wire token compose's text stands for ("" when the text is already
// what Slack wants, as for emoji).
type suggestion struct {
	label, text, wire string
}

// rank returns the indexes of the names matching q, best first: an exact
// match, then a prefix, then a word inside the name, then anywhere;
// shorter names first within each. Case does not matter.
func rank(names []string, q string) []int {
	q = strings.ToLower(q)
	type hit struct{ i, score int }
	var hits []hit
	for i, n := range names {
		if s := score(strings.ToLower(n), q); s >= 0 {
			hits = append(hits, hit{i, s})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		return cmp.Or(cmp.Compare(a.score, b.score), cmp.Compare(len(names[a.i]), len(names[b.i])), cmp.Compare(names[a.i], names[b.i]))
	})
	out := make([]int, len(hits))
	for j, h := range hits {
		out[j] = h.i
	}
	return out
}

// noMatch is score's answer for a name that does not match.
const noMatch = -1

// score grades how name (lower case) matches q (lower case): lower is
// better.
func score(name, q string) int {
	switch {
	case name == q:
		return 0
	case strings.HasPrefix(name, q):
		return 1
	case wordPrefix(name, q):
		return 2
	case strings.Contains(name, q):
		return 3
	}
	return noMatch
}

// wordPrefix says a word of name, after a space, _, - or ., starts with q.
func wordPrefix(name, q string) bool {
	for i, r := range name {
		if strings.ContainsRune(" _-.", r) && strings.HasPrefix(name[i+1:], q) {
			return true
		}
	}
	return false
}

// specialMentions are the mentions that notify groups, with what they do.
var specialMentions = [][2]string{
	{"here", "notify the active members"},
	{"channel", "notify everyone in the channel"},
	{"everyone", "notify the whole workspace"},
}

// peopleSuggestions turns users.search's answer into suggestions: the
// best local matches first, then the rest in the daemon's order (it may
// match on what the TUI does not see, like an email), then @here,
// @channel and @everyone when q starts them.
func peopleSuggestions(users []wire.User, q string) []suggestion {
	lq := strings.ToLower(q)
	best := func(u wire.User) int {
		s := 4
		for _, n := range []string{u.DisplayName, u.RealName, u.Name} {
			if g := score(strings.ToLower(n), lq); n != "" && g >= 0 {
				s = min(s, g)
			}
		}
		return s
	}
	users = slices.Clone(users)
	slices.SortStableFunc(users, func(a, b wire.User) int {
		return cmp.Or(cmp.Compare(best(a), best(b)), cmp.Compare(len(userName(a)), len(userName(b))))
	})
	var out []suggestion
	for _, u := range users {
		out = append(out, personSuggestion(u))
	}
	for _, sp := range specialMentions {
		if strings.HasPrefix(sp[0], lq) {
			out = append(out, suggestion{label: "@" + sp[0] + " · " + sp[1], text: "@" + sp[0], wire: "<!" + sp[0] + ">"})
		}
	}
	return out[:min(len(out), maxSuggestions)]
}

// userName is how a user is called, as State.UserName says it.
func userName(u wire.User) string { return cmp.Or(u.DisplayName, u.RealName, u.Name) }

// personSuggestion shows the name compose will get and, to tell people
// apart, the first other name they have.
func personSuggestion(u wire.User) suggestion {
	name, label := userName(u), userName(u)
	for _, other := range []string{u.RealName, u.Name} {
		if other != "" && other != name {
			label += " · " + other
			break
		}
	}
	return suggestion{label: label, text: "@" + name, wire: "<@" + u.ID + ">"}
}

// channelSuggestions offers the workspace's channels, public and
// private, matching q.
func channelSuggestions(convs []wire.Conversation, q string) []suggestion {
	var chans []wire.Conversation
	for _, c := range convs {
		if c.Kind == "channel" || c.Kind == "private" {
			chans = append(chans, c)
		}
	}
	names := make([]string, len(chans))
	for i, c := range chans {
		names[i] = c.Name
	}
	var out []suggestion
	for _, i := range capped(rank(names, q)) {
		c := chans[i]
		out = append(out, suggestion{label: "#" + c.Name, text: "#" + c.Name, wire: "<#" + c.ID + ">"})
	}
	return out
}

// emojiSuggestions offers the standard and custom emoji matching q. A
// standard one shows its glyph; a custom one, an image the terminal
// cannot draw here, its name only.
func emojiSuggestions(standard, custom []string, q string) []suggestion {
	names := slices.Concat(standard, custom)
	var out []suggestion
	for _, i := range capped(rank(names, q)) {
		code := ":" + names[i] + ":"
		label := code
		if glyph, ok := render.StandardEmoji(names[i]); ok && i < len(standard) {
			label = glyph + " " + code
		}
		out = append(out, suggestion{label: label, text: code})
	}
	return out
}

func capped(idx []int) []int { return idx[:min(len(idx), maxSuggestions)] }

// toWire swaps every display token in text that stands as a whole word
// for its wire token, longest first, so "@Ana Zed" wins over "@Ana".
func toWire(text string, tokens map[string]string) string {
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	var b strings.Builder
	for i := 0; i < len(text); {
		if k, ok := tokenAt(text, i, keys); ok {
			b.WriteString(tokens[k])
			i += len(k)
			continue
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		b.WriteString(text[i : i+size])
		i += size
	}
	return b.String()
}

// tokenAt finds the first of keys that stands as a whole word at text[i:].
func tokenAt(text string, i int, keys []string) (string, bool) {
	if i > 0 {
		if r, _ := utf8.DecodeLastRuneInString(text[:i]); isWordRune(r) {
			return "", false
		}
	}
	for _, k := range keys {
		if !strings.HasPrefix(text[i:], k) {
			continue
		}
		if r, _ := utf8.DecodeRuneInString(text[i+len(k):]); !isWordRune(r) {
			return k, true
		}
	}
	return "", false
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// wireMention matches Slack's <@U1>, <#C1|name> and <!here>.
var wireMention = regexp.MustCompile(`<([@#!])([^>|]*)(?:\|([^>]*))?>`)

var unescape = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")

// toDisplay is toWire's reverse, for loading a message into compose: the
// mentions of people and channels it can name become display tokens,
// recorded in tokens, and Slack's escapes become the characters they
// stand for (after the mentions, so "&lt;@U1&gt;" is not taken for one).
// A mention it cannot name stays as Slack wrote it.
func toDisplay(text string, userName, convName func(id string) string, tokens map[string]string) string {
	out := wireMention.ReplaceAllStringFunc(text, func(match string) string {
		p := wireMention.FindStringSubmatch(match)
		sign, name := displayName(p[1], p[2], p[3], userName, convName)
		if name == "" {
			return match
		}
		tokens[sign+name] = "<" + p[1] + p[2] + ">"
		return sign + name
	})
	return unescape.Replace(out)
}

// displayName is how compose shows the mention <kind id|label>: its sign
// and name, or no name when it cannot say.
func displayName(kind, id, label string, userName, convName func(string) string) (sign, name string) {
	switch kind {
	case "@":
		return "@", cmp.Or(userName(id), label)
	case "#":
		return "#", cmp.Or(convName(id), label)
	}
	return "@", specialName(id)
}

// specialName names <!here>, <!channel> and <!everyone>; "" for others
// (user groups, dates), which stay as they are.
func specialName(id string) string {
	for _, sp := range specialMentions {
		if sp[0] == id {
			return id
		}
	}
	return ""
}

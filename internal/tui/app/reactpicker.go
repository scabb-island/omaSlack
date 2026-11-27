package app

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"gitlab.com/scabb-island/omaslack/internal/mrkdwn"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// The reaction picker remembers the last recentEmoji emoji picked this
// session and lists them first; among equal matches a recent one gains
// recentBoost, below the gap between two match kinds (see fuzzyScore).
const (
	recentEmoji = 12
	recentBoost = 100
)

// reactTo is the message a reaction goes to.
type reactTo struct{ team, conv, ts string }

// rankEmoji keeps the names that match query and orders them: by match
// (fuzzyScore) plus recentBoost for a recent one, then recent ones by
// recency, then in the order given.
func rankEmoji(names, recent []string, query string) []string {
	type scored struct {
		name         string
		score, order int
	}
	var out []scored
	for i, n := range uniq(append(slices.Clone(recent), names...)) {
		s, ok := fuzzyScore(n, query)
		if ok && slices.Contains(recent, n) {
			s += recentBoost
		}
		if ok {
			out = append(out, scored{n, s, i})
		}
	}
	slices.SortFunc(out, func(a, b scored) int { return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.order, b.order)) })
	ranked := make([]string, len(out))
	for i, s := range out {
		ranked[i] = s.name
	}
	return ranked
}

// uniq drops repeated names, keeping the first.
func uniq(names []string) []string {
	seen := map[string]bool{}
	return slices.DeleteFunc(names, func(n string) bool {
		dup := seen[n]
		seen[n] = true
		return dup
	})
}

// reactionMethod is what picking emoji does to msg: remove the user's
// own reaction, else add one. With self unknown it always adds.
func reactionMethod(msg wire.Message, emoji, self string) string {
	for _, r := range msg.Reactions {
		if self != "" && r.Emoji == emoji && slices.Contains(r.Users, self) {
			return "reactions.remove"
		}
	}
	return "reactions.add"
}

// rememberEmoji puts name first among the recent emoji.
func rememberEmoji(recent []string, name string) []string {
	out := append([]string{name}, slices.DeleteFunc(slices.Clone(recent), func(n string) bool { return n == name })...)
	return out[:min(len(out), recentEmoji)]
}

// reactTarget is the selected message of the focused pane (the thread's,
// or else the conversation's), the newest when none is selected.
func (m *Model) reactTarget() (reactTo, bool) {
	msgs, sel := m.messages(), m.msgSel
	if m.focus == threadPane {
		msgs, sel = m.replies(), m.threadSel
	}
	if len(msgs) == 0 {
		return reactTo{}, false
	}
	ts := msgs[len(msgs)-1].TS
	if slices.ContainsFunc(msgs, func(msg wire.Message) bool { return msg.TS == sel }) {
		ts = sel
	}
	return reactTo{m.team, m.openConv(), ts}, true
}

// openReactions opens the reaction picker on the selected message.
func (m *Model) openReactions() tea.Cmd {
	to, ok := m.reactTarget()
	if !ok {
		m.setError("No message to react to.")
		return nil
	}
	m.openPicker(&picker{title: "React", filter: true, items: func(m *Model, query string) []pickItem {
		return m.emojiItems(to, query)
	}})
	return nil
}

// react toggles the reaction emoji (a name, colons optional) on the
// selected message: :react.
func (m *Model) react(emoji string) tea.Cmd {
	emoji = strings.Trim(emoji, ":")
	if emoji == "" {
		m.setError("Usage: :react <emoji>")
		return nil
	}
	to, ok := m.reactTarget()
	if !ok {
		m.setError("No message to react to.")
		return nil
	}
	return m.toggleReaction(to, emoji)
}

// toggleReaction adds emoji to the message, or removes the user's own.
// The daemon's reaction.changed event updates the view.
func (m *Model) toggleReaction(to reactTo, emoji string) tea.Cmd {
	self := directory{st: m.st, team: to.team}.Self()
	method := reactionMethod(m.st.msgs[convKey{to.team, to.conv}][to.ts], emoji, self)
	m.pick.recent = rememberEmoji(m.pick.recent, emoji)
	return call[any](m.client, method, target{to.team, to.conv, ""}.params("ts", to.ts, "emoji", emoji), nil)
}

// emojiItems are the emoji the workspace has, standard then custom,
// ranked for query.
func (m *Model) emojiItems(to reactTo, query string) []pickItem {
	names := append(mrkdwn.StandardEmojiNames(), slices.Sorted(maps.Keys(m.st.ws[to.team].emoji))...)
	known := slices.DeleteFunc(slices.Clone(m.pick.recent), func(n string) bool { return !slices.Contains(names, n) })
	ranked := rankEmoji(names, known, query)
	items := make([]pickItem, len(ranked))
	for i, name := range ranked {
		items[i] = pickItem{
			lines: []string{emojiGlyph(name) + " :" + name + ":"},
			pick:  func(m *Model) tea.Cmd { return m.toggleReaction(to, name) },
		}
	}
	return items
}

// emojiGlyph is a standard emoji's character; custom ones (images) get
// a blank of the same width.
func emojiGlyph(name string) string {
	if s, ok := mrkdwn.StandardEmoji(name); ok {
		return s
	}
	return "  "
}

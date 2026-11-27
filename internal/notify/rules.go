package notify

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/mrkdwn"
)

// Candidate is one new message as the notifier weighs it.
type Candidate struct {
	Team         domain.TeamID
	Self         domain.UserID // the signed-in user; zero when unknown
	Groups       []string      // the user's user groups (S…); nil when unknown
	Conversation domain.Conversation
	Message      domain.Message
	// Following: the message replies in a thread the user follows.
	Following bool
}

// Moment is what the daemon knows when a message arrives.
type Moment struct {
	Started domain.TS // when the daemon started; older messages were pending then
	Now     time.Time
	DND     time.Time // Do Not Disturb is on until then; zero when off
	Viewed  bool      // a client shows the conversation (or thread) focused
}

// Reason is why a message notifies, or Skip.
type Reason uint8

const (
	Skip Reason = iota
	DirectMessage
	GroupMessage
	Mention // of the user, a group of theirs, or @here/@channel/@everyone
	Thread  // a reply in a thread the user follows
	Keyword
	Channel // in a conversation of Config.Channels
	reasonEnd
)

var reasonNames = [reasonEnd]string{
	Skip: "skip", DirectMessage: "dm", GroupMessage: "group dm", Mention: "mention",
	Thread: "thread", Keyword: "keyword", Channel: "channel",
}

func (r Reason) String() string {
	if r >= reasonEnd {
		return ""
	}
	return reasonNames[r]
}

// Decide is why c notifies, or Skip. Nothing notifies when notifications
// are disabled, during Do Not Disturb, while a client views the
// conversation focused, for the user's own messages, for messages from
// before the daemon started or in a muted conversation. Otherwise DMs,
// group DMs, mentions, replies in followed threads, keywords and the
// notify-on-everything channels do.
func Decide(cfg Config, mo Moment, c Candidate) Reason {
	if !cfg.Enabled || mo.silenced() || c.own() || c.Message.TS.Before(mo.Started) || cfg.muted(c) {
		return Skip
	}
	return cfg.match(c)
}

func (mo Moment) silenced() bool { return mo.Viewed || mo.Now.Before(mo.DND) }

func (c Candidate) own() bool {
	u, ok := c.Message.Author.User()
	return ok && u == c.Self
}

func (cfg Config) muted(c Candidate) bool {
	return slices.ContainsFunc(cfg.Muted, func(t Target) bool { return t.Matches(c.Team, c.Conversation) })
}

func (cfg Config) match(c Candidate) Reason {
	switch {
	case c.Conversation.Kind == domain.DirectMessage:
		return DirectMessage
	case c.Conversation.Kind == domain.GroupMessage:
		return GroupMessage
	case c.mentioned():
		return Mention
	case c.Following && c.Message.IsReply():
		return Thread
	case hasKeyword(mrkdwn.Plain(c.Message.Text, nil), cfg.Keywords):
		return Keyword
	case slices.ContainsFunc(cfg.Channels, func(t Target) bool { return t.Matches(c.Team, c.Conversation) }):
		return Channel
	}
	return Skip
}

func (c Candidate) mentioned() bool {
	refs := mrkdwn.References(c.Message.Text)
	return refs.Broadcast ||
		slices.Contains(refs.Users, c.Self.String()) ||
		slices.ContainsFunc(refs.Groups, func(g string) bool { return slices.Contains(c.Groups, g) })
}

// hasKeyword reports whether text holds one of keywords (lower case) as
// whole words: "deploy" matches "the deploy." and "pre-deploy", not
// "deployed".
func hasKeyword(text string, keywords []string) bool {
	text = strings.ToLower(text)
	return slices.ContainsFunc(keywords, func(k string) bool { return containsWord(text, k) })
}

func containsWord(text, word string) bool {
	for i := 0; ; {
		at := strings.Index(text[i:], word)
		if at < 0 {
			return false
		}
		start, end := i+at, i+at+len(word)
		if boundary(text, start, end) {
			return true
		}
		i = start + 1
	}
}

// boundary reports whether text[start:end] has no letter or digit right
// before or after it.
func boundary(text string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(text[:start])
	after, _ := utf8.DecodeRuneInString(text[end:])
	return !wordRune(before) && !wordRune(after)
}

func wordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

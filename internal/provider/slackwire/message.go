package slackwire

import (
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Message is a Slack message as the Web API, the Events API and the web
// client's socket send it.
type Message struct {
	User     string `json:"user"`
	BotID    string `json:"bot_id"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
	Edited   struct {
		TS string `json:"ts"`
	} `json:"edited"`
}

// Parse checks w and builds its domain.Message. The author is the user
// when there is one (apps posting as a person send both) and the bot
// otherwise; a message with neither is rejected.
func (w Message) Parse() (domain.Message, error) {
	ts, err := domain.ParseTS(w.TS)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message ts: %w", err)
	}
	author, err := w.author()
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s author: %w", w.TS, err)
	}
	thread, err := ParseOptionalTS(w.ThreadTS)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s thread_ts: %w", w.TS, err)
	}
	edited, err := ParseOptionalTS(w.Edited.TS)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s edited.ts: %w", w.TS, err)
	}
	return domain.Message{TS: ts, ThreadTS: thread, Author: author, Text: w.Text, Edited: edited}, nil
}

func (w Message) author() (domain.Author, error) {
	if w.User != "" {
		user, err := domain.ParseUserID(w.User)
		return domain.UserAuthor(user), err
	}
	bot, err := domain.ParseBotID(w.BotID)
	return domain.BotAuthor(bot), err
}

// ParseOptionalTS parses s, where an empty s means "no timestamp".
func ParseOptionalTS(s string) (domain.TS, error) {
	if s == "" {
		return domain.TS{}, nil
	}
	return domain.ParseTS(s)
}

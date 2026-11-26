package internalapp

import (
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// wireMessage is a Slack message as the Web API and the Events API send it.
type wireMessage struct {
	User     string `json:"user"`
	BotID    string `json:"bot_id"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
	Edited   struct {
		TS string `json:"ts"`
	} `json:"edited"`
}

// parse checks w and builds its domain.Message. The author is the user
// when there is one (apps posting as a person send both) and the bot
// otherwise; a message with neither is rejected.
func (w wireMessage) parse() (domain.Message, error) {
	ts, err := domain.ParseTS(w.TS)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message ts: %w", err)
	}
	author, err := w.author()
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s author: %w", w.TS, err)
	}
	thread, err := parseOptionalTS(w.ThreadTS)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s thread_ts: %w", w.TS, err)
	}
	edited, err := parseOptionalTS(w.Edited.TS)
	if err != nil {
		return domain.Message{}, fmt.Errorf("message %s edited.ts: %w", w.TS, err)
	}
	return domain.Message{TS: ts, ThreadTS: thread, Author: author, Text: w.Text, Edited: edited}, nil
}

func (w wireMessage) author() (domain.Author, error) {
	if w.User != "" {
		user, err := domain.ParseUserID(w.User)
		return domain.UserAuthor(user), err
	}
	bot, err := domain.ParseBotID(w.BotID)
	return domain.BotAuthor(bot), err
}

// parseOptionalTS parses s, where an empty s means "no timestamp".
func parseOptionalTS(s string) (domain.TS, error) {
	if s == "" {
		return domain.TS{}, nil
	}
	return domain.ParseTS(s)
}

// parseMessages keeps the messages of ws that parse and logs the rest, so
// one odd message does not hide a whole page.
func (p *Provider) parseMessages(ws []wireMessage) []domain.Message {
	out := make([]domain.Message, 0, len(ws))
	for _, w := range ws {
		m, err := w.parse()
		if err != nil {
			p.log.Warn("internal-app: dropped a message", "team", p.team, "err", err)
			continue
		}
		out = append(out, m)
	}
	return out
}

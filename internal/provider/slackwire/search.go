package slackwire

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// searchPageSize is the most hits search.messages returns in one page.
const searchPageSize = 100

// SearchMatch is one hit of search.messages: the message, with the
// conversation it lives in. Hits carry no thread_ts, so a reply found by
// search reads as a top-level message.
type SearchMatch struct {
	Message
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
}

// Parse checks w and builds its domain.SearchHit.
func (w SearchMatch) Parse() (domain.SearchHit, error) {
	conv, err := domain.ParseConversationID(w.Channel.ID)
	if err != nil {
		return domain.SearchHit{}, fmt.Errorf("search hit %s channel: %w", w.TS, err)
	}
	m, err := w.Message.Parse()
	if err != nil {
		return domain.SearchHit{}, err
	}
	return domain.SearchHit{Conversation: conv, Message: m}, nil
}

type searchResponse struct {
	Envelope
	Messages struct {
		Matches []SearchMatch `json:"matches"`
	} `json:"messages"`
}

// SearchMessages calls search.messages for query, in Slack's query syntax,
// and returns the first page of hits, newest first. A limit outside
// 1..100 asks for 100, the most one page holds. Hits that do not parse
// are logged and dropped. It needs the search capability.
func (a *WebAPI) SearchMessages(ctx context.Context, query string, limit int) ([]domain.SearchHit, error) {
	if err := a.caps.Require(domain.Search); err != nil {
		return nil, fmt.Errorf("search.messages: %w", err)
	}
	if limit < 1 || limit > searchPageSize {
		limit = searchPageSize
	}
	form := url.Values{"query": {query}, "count": {strconv.Itoa(limit)}, "sort": {"timestamp"}, "sort_dir": {"desc"}}
	var resp searchResponse
	if err := a.Call(ctx, "search.messages", form, &resp); err != nil {
		return nil, err
	}
	hits := make([]domain.SearchHit, 0, len(resp.Messages.Matches))
	for _, w := range resp.Messages.Matches {
		hit, err := w.Parse()
		if err != nil {
			a.log.Warn("dropped a search hit", "err", err)
			continue
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

// SetPresence calls users.setPresence: away, or auto (active whenever
// the user is online). It needs the presenceWrite capability.
func (a *WebAPI) SetPresence(ctx context.Context, away bool) error {
	if err := a.caps.Require(domain.PresenceWrite); err != nil {
		return fmt.Errorf("users.setPresence: %w", err)
	}
	presence := "auto"
	if away {
		presence = "away"
	}
	return a.Call(ctx, "users.setPresence", url.Values{"presence": {presence}}, &Envelope{})
}

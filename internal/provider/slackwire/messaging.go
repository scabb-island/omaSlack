package slackwire

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// repliesPageSize is how many replies conversations.replies returns per
// page; 200 is the most Slack recommends.
const repliesPageSize = 200

type messagesResponse struct {
	Envelope
	Messages []Message `json:"messages"`
	Metadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// History calls conversations.history.
func (a *WebAPI) History(ctx context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error) {
	form := url.Values{"channel": {conv.String()}, "limit": {strconv.Itoa(limit)}}
	if !before.IsZero() {
		form.Set("latest", before.String())
	}
	var resp messagesResponse
	if err := a.Call(ctx, "conversations.history", form, &resp); err != nil {
		return nil, err
	}
	return a.parseMessages(resp.Messages), nil
}

// Replies calls conversations.replies until the last page. Slack repeats
// the root at the top of every page, so it is filtered out by its ts.
func (a *WebAPI) Replies(ctx context.Context, conv domain.ConversationID, thread domain.TS) ([]domain.Message, error) {
	form := url.Values{"channel": {conv.String()}, "ts": {thread.String()}, "limit": {strconv.Itoa(repliesPageSize)}}
	var replies []domain.Message
	for {
		var resp messagesResponse
		if err := a.Call(ctx, "conversations.replies", form, &resp); err != nil {
			return nil, err
		}
		for _, m := range a.parseMessages(resp.Messages) {
			if m.TS != thread {
				replies = append(replies, m)
			}
		}
		if resp.Metadata.NextCursor == "" {
			return replies, nil
		}
		form.Set("cursor", resp.Metadata.NextCursor)
	}
}

type postResponse struct {
	Envelope
	Message Message `json:"message"`
}

// Post calls chat.postMessage. With a user's credentials the message is
// posted as the user, and Slack answers with the message as stored.
func (a *WebAPI) Post(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) (domain.Message, error) {
	form := url.Values{"channel": {conv.String()}, "text": {text}}
	if !thread.IsZero() {
		form.Set("thread_ts", thread.String())
	}
	var resp postResponse
	if err := a.Call(ctx, "chat.postMessage", form, &resp); err != nil {
		return domain.Message{}, err
	}
	m, err := resp.Message.Parse()
	if err != nil {
		return domain.Message{}, fmt.Errorf("chat.postMessage: %w", err)
	}
	return m, nil
}

type markResponse struct{ Envelope }

// MarkRead calls conversations.mark.
func (a *WebAPI) MarkRead(ctx context.Context, conv domain.ConversationID, upTo domain.TS) error {
	form := url.Values{"channel": {conv.String()}, "ts": {upTo.String()}}
	return a.Call(ctx, "conversations.mark", form, &markResponse{})
}

// parseMessages keeps the messages of ws that parse and logs the rest, so
// one odd message does not hide a whole page.
func (a *WebAPI) parseMessages(ws []Message) []domain.Message {
	out := make([]domain.Message, 0, len(ws))
	for _, w := range ws {
		m, err := w.Parse()
		if err != nil {
			a.log.Warn("dropped a message", "err", err)
			continue
		}
		out = append(out, m)
	}
	return out
}

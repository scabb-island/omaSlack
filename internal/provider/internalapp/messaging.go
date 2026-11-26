package internalapp

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
	envelope
	Messages []wireMessage `json:"messages"`
	Metadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// History calls conversations.history.
func (p *Provider) History(ctx context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error) {
	form := url.Values{"channel": {conv.String()}, "limit": {strconv.Itoa(limit)}}
	if !before.IsZero() {
		form.Set("latest", before.String())
	}
	var resp messagesResponse
	if err := p.call(ctx, "conversations.history", form, &resp); err != nil {
		return nil, err
	}
	return p.parseMessages(resp.Messages), nil
}

// Replies calls conversations.replies until the last page. Slack repeats
// the root at the top of every page, so it is filtered out by its ts.
func (p *Provider) Replies(ctx context.Context, conv domain.ConversationID, thread domain.TS) ([]domain.Message, error) {
	form := url.Values{"channel": {conv.String()}, "ts": {thread.String()}, "limit": {strconv.Itoa(repliesPageSize)}}
	var replies []domain.Message
	for {
		var resp messagesResponse
		if err := p.call(ctx, "conversations.replies", form, &resp); err != nil {
			return nil, err
		}
		for _, m := range p.parseMessages(resp.Messages) {
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
	envelope
	Message wireMessage `json:"message"`
}

// Post calls chat.postMessage. With a user token the message is posted as
// the user, and Slack answers with the message as stored.
func (p *Provider) Post(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) (domain.Message, error) {
	form := url.Values{"channel": {conv.String()}, "text": {text}}
	if !thread.IsZero() {
		form.Set("thread_ts", thread.String())
	}
	var resp postResponse
	if err := p.call(ctx, "chat.postMessage", form, &resp); err != nil {
		return domain.Message{}, err
	}
	m, err := resp.Message.parse()
	if err != nil {
		return domain.Message{}, fmt.Errorf("chat.postMessage: %w", err)
	}
	return m, nil
}

type markResponse struct{ envelope }

// MarkRead calls conversations.mark.
func (p *Provider) MarkRead(ctx context.Context, conv domain.ConversationID, upTo domain.TS) error {
	form := url.Values{"channel": {conv.String()}, "ts": {upTo.String()}}
	return p.call(ctx, "conversations.mark", form, &markResponse{})
}

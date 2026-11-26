package internalapp

import (
	"context"
	"fmt"
	"net/url"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// Conversations lists the conversations the user is a member of, with
// users.conversations. That method carries no read state, and the public
// API has no batch call that does, so each conversation comes back with
// its ID, kind and name only: Latest, LastRead and Mentions stay zero
// (unknown). The sync engine reads each read marker with LastRead, and
// the latest message with History.
func (p *Provider) Conversations(ctx context.Context) ([]domain.Conversation, error) {
	return p.UserConversations(ctx)
}

// The sync engine finds LastRead with a type assertion; this keeps it
// from silently disappearing.
var _ provider.ReadMarkerReader = (*Provider)(nil)

type infoResponse struct {
	slackwire.Envelope
	Channel slackwire.Conversation `json:"channel"`
}

// LastRead returns the user's read marker in conv from conversations.info,
// because the Events API never reports it (see events.go). A conversation
// the user never read has the zero marker. This makes Provider a
// provider.ReadMarkerReader.
func (p *Provider) LastRead(ctx context.Context, conv domain.ConversationID) (domain.TS, error) {
	var resp infoResponse
	if err := p.Call(ctx, "conversations.info", url.Values{"channel": {conv.String()}}, &resp); err != nil {
		return domain.TS{}, err
	}
	c, err := resp.Channel.Parse()
	if err != nil {
		return domain.TS{}, fmt.Errorf("conversations.info: %w", err)
	}
	return c.LastRead, nil
}

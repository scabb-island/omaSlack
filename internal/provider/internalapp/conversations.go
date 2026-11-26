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
// (unknown). The sync engine fills them in with ConversationState, and
// keeps the markers current with LastRead.
func (p *Provider) Conversations(ctx context.Context) ([]domain.Conversation, error) {
	return p.UserConversations(ctx)
}

// The sync engine finds LastRead and ConversationState with type
// assertions; this keeps them from silently disappearing.
var (
	_ provider.ReadMarkerReader        = (*Provider)(nil)
	_ provider.ConversationStateReader = (*Provider)(nil)
)

type infoResponse struct {
	slackwire.Envelope
	Channel slackwire.Conversation `json:"channel"`
}

// LastRead returns the user's read marker in conv from conversations.info,
// because the Events API never reports it (see events.go). A conversation
// the user never read has the zero marker. This makes Provider a
// provider.ReadMarkerReader.
func (p *Provider) LastRead(ctx context.Context, conv domain.ConversationID) (domain.TS, error) {
	c, err := p.ConversationState(ctx, conv)
	return c.LastRead, err
}

// ConversationState returns conv's entry from one conversations.info call:
// kind, name and read marker always; the latest message, and the unread
// count of a DM or group DM as its mentions, when Slack sends them (it
// documents them for DMs; for channels a user token gets only the
// marker, and Latest stays zero). This makes Provider a
// provider.ConversationStateReader.
func (p *Provider) ConversationState(ctx context.Context, conv domain.ConversationID) (domain.Conversation, error) {
	var resp infoResponse
	if err := p.Call(ctx, "conversations.info", url.Values{"channel": {conv.String()}}, &resp); err != nil {
		return domain.Conversation{}, err
	}
	c, err := resp.Channel.Parse()
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("conversations.info: %w", err)
	}
	return c, nil
}

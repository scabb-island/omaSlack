package internalapp

import (
	"context"
	"errors"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Conversations is a placeholder until the conversation list lands (the
// contract gained it for the sync engine's catch-up and cold-start unread
// counts).
func (p *Provider) Conversations(context.Context) ([]domain.Conversation, error) {
	return nil, errors.New("internalapp: conversation list not implemented yet")
}

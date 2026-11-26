package sqlite

import (
	"context"
	"errors"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// errTODO marks the conversation list, which task 2.7 implements.
var errTODO = errors.New("store/sqlite: conversation list not implemented yet (task 2.7)")

// PutConversations implements store.Store.
func (s *Store) PutConversations(context.Context, []domain.Conversation) error { return errTODO }

// Conversations implements store.Store.
func (s *Store) Conversations(context.Context) ([]domain.Conversation, error) { return nil, errTODO }

package syncengine

import (
	"context"
	"errors"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Client actions (task 2.7). The RPC layer calls these for whatever a
// client does; the engine is the single writer, so every action that
// changes Slack also lands in the Store and is published, in that order,
// before the call returns. Provider calls go through the budget at
// Interactive priority. An action the provider cannot do (it lacks the
// optional interface or the capability) returns an error wrapping
// domain.ErrUnsupported. Every action returns ErrStopped when Run is not
// running.

var errTODO = errors.New("syncengine: not implemented yet (task 2.7)")

// State is the workspace's connection state as last published.
func (e *Engine) State() domain.ConnectionState { return domain.Connecting }

// Send posts text to conv (inside thread when it is not zero), stores the
// message, publishes MessageCreated and clears the matching draft.
func (e *Engine) Send(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) (domain.Message, error) {
	return domain.Message{}, errTODO
}

// Edit replaces a message's text, stores it and publishes MessageUpdated.
func (e *Engine) Edit(ctx context.Context, conv domain.ConversationID, ts domain.TS, text string) (domain.Message, error) {
	return domain.Message{}, errTODO
}

// Delete deletes a message, tombstones it and publishes MessageDeleted.
func (e *Engine) Delete(ctx context.Context, conv domain.ConversationID, ts domain.TS) error {
	return errTODO
}

// React adds (add) or removes the user's reaction, stores it and
// publishes ReactionChanged.
func (e *Engine) React(ctx context.Context, conv domain.ConversationID, ts domain.TS, emoji string, add bool) error {
	return errTODO
}

// MarkRead moves the read marker to upTo in Slack and in the Store, and
// publishes ReadChanged and ConversationUpdated. A marker that would move
// back is a no-op (the read state is shared by every client).
func (e *Engine) MarkRead(ctx context.Context, conv domain.ConversationID, upTo domain.TS) error {
	return errTODO
}

// SetDraft stores a draft and publishes DraftChanged. It never reaches
// Slack.
func (e *Engine) SetDraft(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) error {
	return errTODO
}

// SearchMessages, SearchUsers, SetPresence, SnoozeDND and EndDND pass
// through to the provider's optional interface, through the budget. They
// touch neither the Store nor the published events.

func (e *Engine) SearchMessages(ctx context.Context, query string, limit int) ([]domain.SearchHit, error) {
	return nil, errTODO
}

func (e *Engine) SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error) {
	return nil, errTODO
}

func (e *Engine) SetPresence(ctx context.Context, away bool) error { return errTODO }

func (e *Engine) SnoozeDND(ctx context.Context, minutes int) error { return errTODO }

func (e *Engine) EndDND(ctx context.Context) error { return errTODO }

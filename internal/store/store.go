// Package store is the daemon's local cache (spec sync-engine, "Local
// SQLite cache"): one SQLite database per workspace, in WAL mode, with
// versioned migrations. Slack stays the source of truth; deleting the
// database loses nothing but unsent drafts.
package store

import (
	"context"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Reaction is one emoji on a message and who added it, in the order Slack
// reports them.
type Reaction struct {
	Emoji string
	Users []domain.UserID
}

// Store is one workspace's cache. The sync engine is its only writer;
// RPC handlers only read. Implementations must be safe for concurrent use.
//
// Every write is idempotent: applying the same event or page twice, or
// again after a crash halfway through, ends in the same state.
type Store interface {
	// Apply records one domain event; the conversation comes from the event
	// itself. Message events create, update or
	// delete messages; ReactionChanged adds or removes one user's
	// reaction; ReadChanged moves the read marker. Presence, typing and
	// connection events are not stored and are accepted as no-ops.
	Apply(ctx context.Context, ev domain.Event) error

	// PutHistory stores a page of messages fetched from the provider.
	PutHistory(ctx context.Context, conv domain.ConversationID, msgs []domain.Message) error

	// Messages returns up to limit top-level messages and thread roots of
	// conv older than before, newest first. A zero before means "from the
	// newest". Thread replies are not included: they come from Replies.
	Messages(ctx context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error)

	// Replies returns the replies of the thread rooted at thread, oldest
	// first, without the root.
	Replies(ctx context.Context, conv domain.ConversationID, thread domain.TS) ([]domain.Message, error)

	// Reactions returns the reactions on one message.
	Reactions(ctx context.Context, conv domain.ConversationID, ts domain.TS) ([]Reaction, error)

	// LastRead returns the read marker of conv (zero if never read).
	LastRead(ctx context.Context, conv domain.ConversationID) (domain.TS, error)

	// Draft returns the unsent draft of conv, or of the thread rooted at
	// thread when thread is not zero. No draft is the empty string.
	Draft(ctx context.Context, conv domain.ConversationID, thread domain.TS) (string, error)

	// SetDraft stores the draft; an empty text deletes it.
	SetDraft(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) error

	// Close releases the database.
	Close() error
}

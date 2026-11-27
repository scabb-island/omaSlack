// Package store is the daemon's local cache (spec sync-engine, "Local
// SQLite cache"): one SQLite database per workspace, in WAL mode, with
// versioned migrations. Slack stays the source of truth; deleting the
// database loses nothing but unsent drafts.
package store

import (
	"context"
	"errors"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Reaction is one emoji on a message and who added it, in the order Slack
// reports them.
type Reaction struct {
	Emoji string
	Users []domain.UserID
}

// ErrUnknownFile reports a file no stored message carries.
var ErrUnknownFile = errors.New("store: unknown file")

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
	//
	// Messages, Replies and PutHistory carry each message's files. A
	// version of a message that carries no files keeps the stored ones
	// (an edit's result may leave them out); one that carries files
	// replaces them, under the same rule as the rest of the message: never
	// with an older version.
	PutHistory(ctx context.Context, conv domain.ConversationID, msgs []domain.Message) error

	// Messages returns up to limit top-level messages and thread roots of
	// conv older than before, newest first. A zero before means "from the
	// newest". Thread replies are not included: they come from Replies.
	Messages(ctx context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error)

	// Oldest returns the timestamp of the oldest message Messages would
	// return for conv (zero when there is none): where "load older" continues.
	Oldest(ctx context.Context, conv domain.ConversationID) (domain.TS, error)

	// Replies returns the replies of the thread rooted at thread, oldest
	// first, without the root.
	Replies(ctx context.Context, conv domain.ConversationID, thread domain.TS) ([]domain.Message, error)

	// CountReply counts reply, which arrived in real time, on its thread
	// root: one reply more, and reply as the newest. It returns the root
	// as now stored and true, or false when nothing changed: reply is not
	// a reply, its root is not cached (or deleted), or reply is not newer
	// than the root's newest known reply (a replay, or Slack's echo of the
	// user's own reply).
	CountReply(ctx context.Context, conv domain.ConversationID, reply domain.Message) (domain.Message, bool, error)

	// File returns a file some stored message carries, with its download
	// URL; ErrUnknownFile when none does.
	File(ctx context.Context, id domain.FileID) (domain.File, error)

	// Reactions returns the reactions on one message.
	Reactions(ctx context.Context, conv domain.ConversationID, ts domain.TS) ([]Reaction, error)

	// LastRead returns the read marker of conv (zero if never read).
	LastRead(ctx context.Context, conv domain.ConversationID) (domain.TS, error)

	// PutConversations records the provider's conversation list (task
	// 2.7): kind, name, latest message and unread mentions of each entry.
	// It replaces the previous list, so a conversation the user left
	// disappears. Read markers in the list go through the same
	// forward-only rule as ReadChanged.
	PutConversations(ctx context.Context, list []domain.Conversation) error

	// UpdateConversation replaces one entry of the stored list with c
	// (kind, name, latest message and unread mentions), for read state
	// learned one conversation at a time. A conversation not in the list
	// is left out: the list decides which ones the user is in. Its read
	// marker follows the same forward-only rule.
	UpdateConversation(ctx context.Context, c domain.Conversation) error

	// Conversations returns the stored list. Each entry's LastRead is the
	// stored read marker, and its Latest is the newer of the list's and
	// the newest stored top-level message, so messages that arrived since
	// the last list count as unread. Mentions drop to zero once the read
	// marker reaches Latest.
	Conversations(ctx context.Context) ([]domain.Conversation, error)

	// Draft returns the unsent draft of conv, or of the thread rooted at
	// thread when thread is not zero. No draft is the empty string.
	Draft(ctx context.Context, conv domain.ConversationID, thread domain.TS) (string, error)

	// SetDraft stores the draft; an empty text deletes it.
	SetDraft(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) error

	// Close releases the database.
	Close() error
}

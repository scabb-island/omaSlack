// Package provider defines how the daemon talks to Slack (spec
// slack-connection, "Interchangeable connection providers"). Each workspace
// has exactly one Provider; desktop-session and internal-app are its two
// implementations. Everything a Provider returns is already parsed into
// domain types: the provider is the boundary where Slack data is checked.
package provider

import (
	"context"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Provider is one workspace's connection to Slack.
//
// Operations that need a capability the provider does not declare return an
// error wrapping domain.ErrUnsupported.
type Provider interface {
	// Team is the workspace this provider is bound to.
	Team() domain.TeamID

	// Capabilities is what this provider supports. It does not change while
	// the provider is alive.
	Capabilities() domain.Capabilities

	// Connect opens the real-time stream. Events arrive normalized and
	// carry this provider's team. The channel closes when ctx ends or the
	// connection is lost for good; reconnecting is the caller's job, so
	// one backoff policy lives in the sync engine.
	Connect(ctx context.Context) (<-chan domain.Event, error)

	// History returns up to limit messages older than before (newest
	// first). A zero before means "from the latest message".
	History(ctx context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error)

	// Replies returns the replies of the thread rooted at thread, oldest
	// first, without the root.
	Replies(ctx context.Context, conv domain.ConversationID, thread domain.TS) ([]domain.Message, error)

	// Post sends text to conv, inside thread when thread is not zero, and
	// returns the message as Slack stored it.
	Post(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) (domain.Message, error)

	// MarkRead moves the user's read marker in conv to upTo.
	MarkRead(ctx context.Context, conv domain.ConversationID, upTo domain.TS) error

	// Conversations lists the conversations the user is a member of, with
	// their latest message, read marker and unread mentions when the
	// provider knows them. The sync engine uses it to find what needs a
	// catch-up after an outage and to seed unread counts on a cold start.
	Conversations(ctx context.Context) ([]domain.Conversation, error)
}

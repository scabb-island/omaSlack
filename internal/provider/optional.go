package provider

import (
	"context"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Optional abilities. A provider implements them only when its protocol
// allows it; callers find them with a type assertion and must cope with
// their absence.

// ReadMarkerReader is implemented by a provider whose stream does not
// carry the user's read markers (the internal-app provider: those events
// are RTM-only). The sync engine polls LastRead instead.
type ReadMarkerReader interface {
	LastRead(ctx context.Context, conv domain.ConversationID) (domain.TS, error)
}

// PresenceSubscriber is implemented by a provider whose stream reports
// presence only for users it subscribed to (the desktop-session web
// protocol). Each call replaces the previous subscription.
type PresenceSubscriber interface {
	SubscribePresence(ctx context.Context, users []domain.UserID) error
}

// TypingSender is implemented by a provider that can tell the other people
// in conv that the user is typing. It is declared by the typing capability.
type TypingSender interface {
	SendTyping(ctx context.Context, conv domain.ConversationID) error
}

// The writes below are what clients do to Slack beyond posting (task 2.6).
// Both providers implement them; they stay optional so a provider whose
// protocol lacks one simply does not, and the daemon answers
// -32002 unsupported_capability.

// MessageEditor edits and deletes the user's own messages.
type MessageEditor interface {
	// Edit replaces the text of the message at ts and returns the message
	// as Slack stored it (with Edited set).
	Edit(ctx context.Context, conv domain.ConversationID, ts domain.TS, text string) (domain.Message, error)
	// Delete deletes the message at ts.
	Delete(ctx context.Context, conv domain.ConversationID, ts domain.TS) error
}

// Reactor adds and removes the user's reactions. emoji is the name
// without colons ("thumbsup", "+1", "smile::skin-tone-2").
type Reactor interface {
	AddReaction(ctx context.Context, conv domain.ConversationID, ts domain.TS, emoji string) error
	RemoveReaction(ctx context.Context, conv domain.ConversationID, ts domain.TS, emoji string) error
}

// MessageSearcher searches messages with Slack's query syntax. It is
// declared by the search capability. Hits come newest first.
type MessageSearcher interface {
	SearchMessages(ctx context.Context, query string, limit int) ([]domain.SearchHit, error)
}

// UserSearcher finds people by handle, real name or display name.
type UserSearcher interface {
	SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error)
}

// PresenceSetter sets the user's own presence: auto (active when online)
// or away. It is declared by the presence_write capability.
type PresenceSetter interface {
	SetPresence(ctx context.Context, away bool) error
}

// DNDController snoozes notifications for minutes, or ends the snooze.
type DNDController interface {
	SnoozeDND(ctx context.Context, minutes int) error
	EndDND(ctx context.Context) error
}

package provider

import (
	"context"
	"io"

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

// ConversationStateReader is implemented by a provider whose conversation
// list carries no read state (the internal-app provider: users.conversations
// has no latest message, read marker or mention count, and no batch call
// that has them accepts its token). The sync engine fills the list in with
// it, one conversation at a time (spec sync-engine, "Provider list without
// read state").
type ConversationStateReader interface {
	// ConversationState returns conv's entry with whatever read state one
	// call reports: its read marker, its latest message (zero when the
	// call does not say: the caller asks History), and its unread
	// mentions when known.
	ConversationState(ctx context.Context, conv domain.ConversationID) (domain.Conversation, error)
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

// Upload is one local file to share: the name Slack shows, its exact
// size in bytes, and its content, which must hold exactly Size bytes.
type Upload struct {
	Name    string
	Size    int64
	Content io.Reader
}

// FileUploader shares files in conv, inside thread when it is not zero,
// as one message with caption as its text (empty for none). It is
// declared by the files capability. It returns the files as Slack stored
// them; the message itself arrives on the stream, as Slack shares the
// files after the call returns.
type FileUploader interface {
	UploadFiles(ctx context.Context, conv domain.ConversationID, thread domain.TS, files []Upload, caption string) ([]domain.File, error)
}

// FileDownloader streams a file's bytes into w with the provider's
// credentials, and returns how many it wrote. A file larger than max
// bytes fails with ErrFileTooLarge, having written at most max+1 bytes.
// It is declared by the files capability.
type FileDownloader interface {
	DownloadFile(ctx context.Context, f domain.File, w io.Writer, max int64) (int64, error)
}

// UserDirectory resolves user IDs to people, for names in messages,
// mentions and direct-message titles (task 3.8). Unknown IDs are left out
// of the result, not an error. Implementations cache: clients ask for the
// same few people over and over.
type UserDirectory interface {
	Users(ctx context.Context, ids []domain.UserID) ([]domain.User, error)
}

// EmojiLister lists the workspace's custom emoji (task 3.8), declared by
// the custom_emoji capability: name (no colons) to an image URL, or
// "alias:<name>" for an alias.
type EmojiLister interface {
	Emoji(ctx context.Context) (map[string]string, error)
}

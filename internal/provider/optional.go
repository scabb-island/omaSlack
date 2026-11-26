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

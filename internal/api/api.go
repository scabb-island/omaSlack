// Package api answers the daemon's v1 methods (spec daemon-api, "Messaging
// and state methods"; task 2.3). It parses params once, at this boundary,
// routes each call to the workspace it names, and returns the shapes of
// package wire. It holds no state of its own: reads come from each
// workspace's Store, and everything else goes through its sync engine,
// the single writer.
//
// Errors clients see:
//   - -32602 Invalid params: params that do not parse, a bad ID or TS, or
//     a team that is not signed in ("unknown workspace").
//   - -32002 unsupported_capability: the provider lacks what the method
//     needs (anything wrapping domain.ErrUnsupported).
//   - -32003 auth_expired: Slack rejected the credentials (anything
//     wrapping provider.ErrAuthExpired).
package api

import (
	"context"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// Reader is what the methods read from a workspace's cache: the read-only
// part of store.Store.
type Reader interface {
	Conversations(ctx context.Context) ([]domain.Conversation, error)
	Messages(ctx context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error)
	Replies(ctx context.Context, conv domain.ConversationID, thread domain.TS) ([]domain.Message, error)
	Reactions(ctx context.Context, conv domain.ConversationID, ts domain.TS) ([]store.Reaction, error)
	Draft(ctx context.Context, conv domain.ConversationID, thread domain.TS) (string, error)
}

// Actions is what the methods ask a workspace's sync engine to do.
type Actions interface {
	State() domain.ConnectionState
	OpenConversation(ctx context.Context, conv domain.ConversationID) error
	CloseConversation(ctx context.Context, conv domain.ConversationID) error
	OpenThread(ctx context.Context, conv domain.ConversationID, thread domain.TS) error
	LoadOlder(ctx context.Context, conv domain.ConversationID) (int, error)
	Send(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) (domain.Message, error)
	Edit(ctx context.Context, conv domain.ConversationID, ts domain.TS, text string) (domain.Message, error)
	Delete(ctx context.Context, conv domain.ConversationID, ts domain.TS) error
	React(ctx context.Context, conv domain.ConversationID, ts domain.TS, emoji string, add bool) error
	MarkRead(ctx context.Context, conv domain.ConversationID, upTo domain.TS) error
	SetDraft(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) error
	SearchMessages(ctx context.Context, query string, limit int) ([]domain.SearchHit, error)
	SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error)
	SetPresence(ctx context.Context, away bool) error
	SnoozeDND(ctx context.Context, minutes int) error
	EndDND(ctx context.Context) error
}

// The real implementations satisfy the contracts.
var (
	_ Reader  = store.Store(nil)
	_ Actions = (*syncengine.Engine)(nil)
)

// Workspace is one signed-in workspace as the methods see it.
type Workspace struct {
	Team         domain.TeamID
	Name         string // the workspace's display name
	Provider     string // "desktop-session" or "internal-app"
	Capabilities domain.Capabilities
	Store        Reader
	Engine       Actions
}

// Info is the workspace as workspaces.list and hello report it.
func (w Workspace) Info() wire.Workspace {
	return wire.FromWorkspace(w.Team, w.Name, w.Provider, w.Capabilities, w.Engine.State())
}

// Directory lists the signed-in workspaces. The daemon owns it; the list
// can change while the daemon runs (auth add, auth remove).
type Directory interface {
	// Workspaces returns every workspace, ordered by team ID.
	Workspaces() []Workspace
}

// Focuser delivers ui.focus to the clients subscribed to it (the events
// hub) and reports how many it reached.
type Focuser interface {
	Focus(f wire.FocusEvent) int
}

// Methods returns the v1 methods, keyed by name, ready to merge into the
// daemon's rpc.Router next to events.subscribe:
//
//	workspaces.list{}                                  → []wire.Workspace
//	conversations.list{team, filter?: "all"|"unread"}  → []wire.Conversation
//	conversations.open{team, conv}                     → {messages: []wire.Message, draft}
//	conversations.close{team, conv}                    → null
//	conversations.markRead{team, conv, ts}             → null
//	messages.history{team, conv, before?, limit?}      → []wire.Message (newest first)
//	messages.replies{team, conv, threadTS}             → []wire.Message (oldest first)
//	messages.send{team, conv, threadTS?, text}         → wire.Message
//	messages.edit{team, conv, ts, text}                → wire.Message
//	messages.delete{team, conv, ts}                    → null
//	reactions.add{team, conv, ts, emoji}               → null
//	reactions.remove{team, conv, ts, emoji}            → null
//	drafts.get{team, conv, threadTS?}                  → {text}
//	drafts.set{team, conv, threadTS?, text}            → null
//	search.messages{team, query, limit?}               → []wire.SearchHit
//	users.search{team, q, limit?}                      → []wire.User
//	presence.set{team, presence: "auto"|"away"}        → null
//	dnd.snooze{team, minutes}                          → null
//	dnd.end{team}                                      → null
//	unread.summary{}                                   → wire.Summary
//	ui.focus{team, conv, ts?}                          → {clients: n}
//
// files.upload and files.fetch arrive with task 2.9.
func Methods(dir Directory, focus Focuser) rpc.Router {
	return rpc.Router{} // task 2.3
}

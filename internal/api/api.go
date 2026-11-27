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
	UploadFiles(ctx context.Context, conv domain.ConversationID, thread domain.TS, paths []string, caption string) ([]domain.File, error)
	FetchFile(ctx context.Context, id domain.FileID) (string, error)
	Users(ctx context.Context, ids []domain.UserID) ([]domain.User, error)
	Emoji(ctx context.Context) (map[string]string, error)
	SendTyping(ctx context.Context, conv domain.ConversationID) error
}

// The real implementations satisfy the contracts.
var (
	_ Reader  = store.Store(nil)
	_ Actions = (*syncengine.Engine)(nil)
)

// Workspace is one signed-in workspace as the methods see it.
type Workspace struct {
	Team         domain.TeamID
	Name         string        // the workspace's display name
	Self         domain.UserID // the signed-in user; zero when unknown
	Provider     string        // "desktop-session" or "internal-app"
	Capabilities domain.Capabilities
	Store        Reader
	Engine       Actions
}

// Info is the workspace as workspaces.list and hello report it.
func (w Workspace) Info() wire.Workspace {
	return wire.FromWorkspace(w.Team, w.Name, w.Self, w.Provider, w.Capabilities, w.Engine.State())
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
//	files.upload{team, conv, threadTS?, paths, caption?} → {files: []wire.File}
//	files.fetch{team, fileId}                          → {localPath}
//	users.info{team, ids}                              → []wire.User (unknown IDs left out)
//	emoji.list{team}                                   → wire.EmojiList
//	typing.send{team, conv}                            → null
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
// Params:
//   - team, conv: Slack IDs; ts, threadTS, before: Slack timestamps
//     ("1700000000.000100"). Any other member is ignored, and a member set
//     to null counts as absent, so older daemons accept newer clients.
//   - limit: messages.history defaults to HistoryLimit (50) and is capped
//     at HistoryMaxLimit (200); search.messages and users.search default
//     to SearchLimit (20) and are capped at SearchMaxLimit (100). A larger
//     limit is lowered to the cap; below 1 is -32602.
//   - text: required; empty is -32602 except in drafts.set, where it
//     clears the draft. query and q must not be empty. emoji is a name;
//     colons around it (":tada:") are dropped. minutes is at least 1.
//   - paths: a non-empty array of absolute paths on the daemon's machine
//     (the daemon runs as the same user, but not in the client's working
//     directory). caption is optional text. fileId is a Slack file ID
//     (F...), from the files of a message.
//   - ids: a non-empty array of user IDs (U... or W...), at most
//     UsersInfoMaxIDs (100); more is -32602, not lowered.
//
// Behavior:
//   - messages.history reads the cache; when it holds fewer than limit
//     messages before the cursor, it asks the engine for one older page
//     (LoadOlder) and reads again. Every message carries its reactions.
//   - conversations.open opens the conversation in the engine, then
//     returns the newest HistoryLimit messages and the draft.
//   - messages.replies opens the thread in the engine, then reads the
//     cache.
//   - search.messages needs domain.Search, presence.set needs
//     domain.PresenceWrite, files.upload and files.fetch need
//     domain.Files, emoji.list needs domain.CustomEmoji and typing.send
//     needs domain.Typing; without it they answer -32002 and the engine is
//     not called.
//   - users.info answers the known people among ids, in the order asked
//     and once each; an ID nobody knows is left out, not an error. The
//     provider names them from its cached users.list directory (loaded
//     once per 30 minutes, so the first call of a workspace waits for that
//     load) and looks up at most 20 others per call with users.info; a
//     client asks again for the ones still missing.
//   - emoji.list answers the custom emoji by name (no colons): an image
//     URL, or "alias:<name>" for an alias. The provider keeps it an hour.
//   - typing.send tells the conversation the user is typing; Slack shows
//     it for a few seconds, so clients repeat it while the user types.
//     It needs the live connection: between reconnects it fails.
//   - files.upload shares the files as one message (with caption as its
//     text) and answers with the files as Slack stored them. The message
//     itself arrives as a message.created event, carrying the same files.
//   - files.fetch downloads a file of a cached message into the daemon's
//     cache, once, and answers with its path there. The path is only
//     valid on this machine; clients open or copy it, never send it on.
//   - A file the request cannot use is -32602 with what is wrong: an
//     unknown file, a file Slack offers no download for, a file over the
//     size limit, an upload path that does not exist, cannot be read or
//     is not a regular file.
func Methods(dir Directory, focus Focuser) rpc.Router {
	methods := map[string]method{
		"workspaces.list":        workspacesList(dir),
		"conversations.list":     conversationsList,
		"conversations.open":     conversationsOpen,
		"conversations.close":    conversationsClose,
		"conversations.markRead": conversationsMarkRead,
		"messages.history":       messagesHistory,
		"messages.replies":       messagesReplies,
		"messages.send":          messagesSend,
		"messages.edit":          messagesEdit,
		"messages.delete":        messagesDelete,
		"reactions.add":          react(true),
		"reactions.remove":       react(false),
		"files.upload":           filesUpload,
		"files.fetch":            filesFetch,
		"users.info":             usersInfo,
		"emoji.list":             emojiList,
		"typing.send":            typingSend,
		"drafts.get":             draftsGet,
		"drafts.set":             draftsSet,
		"search.messages":        searchMessages,
		"users.search":           usersSearch,
		"presence.set":           presenceSet,
		"dnd.snooze":             dndSnooze,
		"dnd.end":                dndEnd,
		"unread.summary":         unreadSummary(dir),
		"ui.focus":               uiFocus(focus),
	}
	r := make(rpc.Router, len(methods))
	for name, m := range methods {
		r[name] = handler(dir, m)
	}
	return r
}

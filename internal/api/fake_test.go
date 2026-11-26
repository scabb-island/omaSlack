package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// fakeStore is one workspace's cache in memory. Only the engine fake
// writes to it, as in the daemon.
type fakeStore struct {
	mu        sync.Mutex
	convs     []domain.Conversation
	messages  map[domain.ConversationID][]domain.Message // any order
	reactions map[string][]store.Reaction                // by conv/ts
	drafts    map[string]string                          // by conv/thread
	listErr   error                                      // what Conversations fails with
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		messages:  map[domain.ConversationID][]domain.Message{},
		reactions: map[string][]store.Reaction{},
		drafts:    map[string]string{},
	}
}

func key(conv domain.ConversationID, ts domain.TS) string { return conv.String() + "/" + ts.String() }

// errDisk is a cache failure. Every read of C0BROKEN fails with it, and so
// does Conversations when listErr is set. Its text must never reach a
// client.
var errDisk = errors.New("sqlite: /home/someone/.local/state/omaslack/T0ACME.db: disk I/O error")

func broken(conv domain.ConversationID) error {
	if conv.String() == "C0BROKEN" {
		return errDisk
	}
	return nil
}

func (s *fakeStore) Conversations(context.Context) ([]domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.convs), s.listErr
}

func (s *fakeStore) Messages(_ context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error) {
	if err := broken(conv); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Message
	for _, m := range s.newestFirst(conv) {
		if !m.IsReply() && (before.IsZero() || m.TS.Before(before)) && len(out) < limit {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *fakeStore) Replies(_ context.Context, conv domain.ConversationID, thread domain.TS) ([]domain.Message, error) {
	if err := broken(conv); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Message
	for _, m := range s.newestFirst(conv) {
		if m.IsReply() && m.ThreadTS == thread {
			out = append([]domain.Message{m}, out...)
		}
	}
	return out, nil
}

func (s *fakeStore) newestFirst(conv domain.ConversationID) []domain.Message {
	msgs := slices.Clone(s.messages[conv])
	slices.SortFunc(msgs, func(a, b domain.Message) int { return strings.Compare(b.TS.String(), a.TS.String()) })
	return msgs
}

func (s *fakeStore) Reactions(_ context.Context, conv domain.ConversationID, ts domain.TS) ([]store.Reaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reactions[key(conv, ts)], broken(conv)
}

func (s *fakeStore) Draft(_ context.Context, conv domain.ConversationID, thread domain.TS) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drafts[key(conv, thread)], broken(conv)
}

func (s *fakeStore) add(conv domain.ConversationID, msgs ...domain.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[conv] = append(s.messages[conv], msgs...)
}

// fakeEngine records every action as one line, and answers from canned
// data. With err set, every action records itself and fails with it.
type fakeEngine struct {
	store *fakeStore
	older map[domain.ConversationID][]domain.Message // what LoadOlder fetches
	err   error

	mu    sync.Mutex
	calls []string
}

func (e *fakeEngine) record(format string, args ...any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, fmt.Sprintf(format, args...))
	return e.err
}

func (e *fakeEngine) recorded() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.calls)
}

func (e *fakeEngine) State() domain.ConnectionState { return domain.Connected }

func (e *fakeEngine) OpenConversation(_ context.Context, conv domain.ConversationID) error {
	return e.record("OpenConversation %s", conv)
}

func (e *fakeEngine) CloseConversation(_ context.Context, conv domain.ConversationID) error {
	return e.record("CloseConversation %s", conv)
}

func (e *fakeEngine) OpenThread(_ context.Context, conv domain.ConversationID, thread domain.TS) error {
	return e.record("OpenThread %s %s", conv, thread)
}

func (e *fakeEngine) LoadOlder(_ context.Context, conv domain.ConversationID) (int, error) {
	if err := e.record("LoadOlder %s", conv); err != nil {
		return 0, err
	}
	page := e.older[conv]
	delete(e.older, conv)
	e.store.add(conv, page...)
	return len(page), nil
}

func (e *fakeEngine) Send(_ context.Context, conv domain.ConversationID, thread domain.TS, text string) (domain.Message, error) {
	err := e.record("Send %s thread=%s %q", conv, show(thread), text)
	return domain.Message{TS: ts("1700000900.000000"), ThreadTS: thread, Author: author("U0ME"), Text: text}, err
}

func (e *fakeEngine) Edit(_ context.Context, conv domain.ConversationID, at domain.TS, text string) (domain.Message, error) {
	err := e.record("Edit %s %s %q", conv, at, text)
	return domain.Message{TS: at, Author: author("U0ME"), Text: text, Edited: ts("1700000950.000000")}, err
}

func (e *fakeEngine) Delete(_ context.Context, conv domain.ConversationID, at domain.TS) error {
	return e.record("Delete %s %s", conv, at)
}

func (e *fakeEngine) React(_ context.Context, conv domain.ConversationID, at domain.TS, emoji string, add bool) error {
	return e.record("React %s %s %s add=%t", conv, at, emoji, add)
}

func (e *fakeEngine) MarkRead(_ context.Context, conv domain.ConversationID, upTo domain.TS) error {
	return e.record("MarkRead %s %s", conv, upTo)
}

func (e *fakeEngine) SetDraft(_ context.Context, conv domain.ConversationID, thread domain.TS, text string) error {
	return e.record("SetDraft %s thread=%s %q", conv, show(thread), text)
}

// show prints a timestamp for the golden files, "-" for none.
func show(t domain.TS) string {
	if t.IsZero() {
		return "-"
	}
	return t.String()
}

func (e *fakeEngine) SearchMessages(_ context.Context, query string, limit int) ([]domain.SearchHit, error) {
	err := e.record("SearchMessages %q limit=%d", query, limit)
	hit := domain.SearchHit{Conversation: conv("C0GENERAL"), Message: domain.Message{TS: ts("1700000100.000000"), Author: author("U0ANA"), Text: "deploy is done"}}
	return []domain.SearchHit{hit}, err
}

func (e *fakeEngine) SearchUsers(_ context.Context, query string, limit int) ([]domain.User, error) {
	err := e.record("SearchUsers %q limit=%d", query, limit)
	return []domain.User{{ID: user("U0ANA"), Name: "ana", RealName: "Ana Pérez"}}, err // é: intentional non-ASCII
}

func (e *fakeEngine) SetPresence(_ context.Context, away bool) error {
	return e.record("SetPresence away=%t", away)
}

func (e *fakeEngine) SnoozeDND(_ context.Context, minutes int) error {
	return e.record("SnoozeDND %d", minutes)
}

func (e *fakeEngine) EndDND(context.Context) error { return e.record("EndDND") }

// fakeDirectory is a fixed list of workspaces.
type fakeDirectory []Workspace

func (d fakeDirectory) Workspaces() []Workspace { return d }

// fakeFocus records ui.focus requests and reaches a fixed number of
// clients.
type fakeFocus struct {
	mu   sync.Mutex
	sent []wire.FocusEvent
}

func (f *fakeFocus) Focus(ev wire.FocusEvent) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, ev)
	return 2
}

func ts(s string) domain.TS {
	t, err := domain.ParseTS(s)
	if err != nil {
		panic(err)
	}
	return t
}

func conv(s string) domain.ConversationID {
	c, err := domain.ParseConversationID(s)
	if err != nil {
		panic(err)
	}
	return c
}

func user(s string) domain.UserID {
	u, err := domain.ParseUserID(s)
	if err != nil {
		panic(err)
	}
	return u
}

func team(s string) domain.TeamID {
	t, err := domain.ParseTeamID(s)
	if err != nil {
		panic(err)
	}
	return t
}

func author(s string) domain.Author { return domain.UserAuthor(user(s)) }

// errExpired is what an engine returns once Slack rejected the
// credentials: provider.ErrAuthExpired, wrapped.
var errExpired = fmt.Errorf("syncengine: %w", fmt.Errorf("chat.postMessage: invalid_auth: %w", provider.ErrAuthExpired))

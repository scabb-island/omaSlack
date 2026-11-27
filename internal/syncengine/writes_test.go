package syncengine_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
)

// me is the signed-in user (syncengine.Config.Self).
var me = must(domain.ParseUserID("U0ME"))

// writes is the rig's fake plus every optional write a provider may
// have. Post answers with the message at postAt by me; the other writes
// record "method args" and fail with what fail holds for their method.
type writes struct {
	*providertest.Fake
	caps   domain.Capabilities
	postAt domain.TS
	hits   []domain.SearchHit
	users  []domain.User
	served map[domain.FileID]string // what DownloadFile serves instead of contentOf

	// dnd is what dnd.info answers; snoozeEnd what dnd.setSnooze does.
	// presence is what users.getPresence answers.
	dnd, snoozeEnd time.Time
	presence       domain.Presence

	mu       sync.Mutex
	calls    []string
	dndReads int
	fail     map[string]error
}

var (
	_ provider.MessageEditor   = (*writes)(nil)
	_ provider.Reactor         = (*writes)(nil)
	_ provider.MessageSearcher = (*writes)(nil)
	_ provider.UserSearcher    = (*writes)(nil)
	_ provider.PresenceSetter  = (*writes)(nil)
	_ provider.DNDController   = (*writes)(nil)
	_ provider.PresenceReader  = (*writes)(nil)
	_ provider.UserDirectory   = (*writes)(nil)
	_ provider.EmojiLister     = (*writes)(nil)
	_ provider.TypingSender    = (*writes)(nil)
)

// newWritesRig is a rig whose provider can do every write and declares
// caps.
func newWritesRig(t *testing.T, caps ...domain.Capability) (*rig, *writes) {
	t.Helper()
	var w *writes
	r := newRigWith(t, func(f *providertest.Fake) provider.Provider {
		w = &writes{Fake: f, caps: domain.NewCapabilities(caps...), postAt: ts4, fail: map[string]error{}, presence: domain.Active}
		return w
	})
	return r, w
}

// bare hides every optional interface of the provider it wraps.
type bare struct{ provider.Provider }

func newBareRig(t *testing.T) *rig {
	t.Helper()
	return newRigWith(t, func(f *providertest.Fake) provider.Provider { return bare{f} })
}

func (w *writes) Capabilities() domain.Capabilities { return w.caps }

func (w *writes) Post(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) (domain.Message, error) {
	m, err := w.Fake.Post(ctx, conv, thread, text)
	m.TS, m.Author = w.postAt, domain.UserAuthor(me)
	return m, err
}

func (w *writes) record(method string, args ...any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, fmt.Sprintln(append([]any{method}, args...)...))
	err := w.fail[method]
	delete(w.fail, method)
	return err
}

func (w *writes) recorded() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.calls)
}

func (w *writes) failNext(method string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fail[method] = err
}

func (w *writes) Edit(_ context.Context, conv domain.ConversationID, ts domain.TS, text string) (domain.Message, error) {
	// chat.update does not return the thread: the engine must not lose it.
	return domain.Message{TS: ts, Text: text, Edited: ts4}, w.record("chat.update", conv, ts, text)
}

func (w *writes) Delete(_ context.Context, conv domain.ConversationID, ts domain.TS) error {
	return w.record("chat.delete", conv, ts)
}

func (w *writes) AddReaction(_ context.Context, conv domain.ConversationID, ts domain.TS, emoji string) error {
	return w.record("reactions.add", conv, ts, emoji)
}

func (w *writes) RemoveReaction(_ context.Context, conv domain.ConversationID, ts domain.TS, emoji string) error {
	return w.record("reactions.remove", conv, ts, emoji)
}

func (w *writes) SearchMessages(_ context.Context, query string, limit int) ([]domain.SearchHit, error) {
	return w.hits, w.record("search.messages", query, limit)
}

func (w *writes) SearchUsers(_ context.Context, query string, limit int) ([]domain.User, error) {
	return w.users, w.record("users.search", query, limit)
}

func (w *writes) SetPresence(_ context.Context, away bool) error {
	return w.record("users.setPresence", away)
}

func (w *writes) SnoozeDND(_ context.Context, minutes int) (time.Time, error) {
	return w.snoozeEnd, w.record("dnd.setSnooze", minutes)
}

// DND is counted apart from the other calls: every connection reads it.
func (w *writes) DND(context.Context) (time.Time, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dndReads++
	err := w.fail["dnd.info"]
	delete(w.fail, "dnd.info")
	return w.dnd, err
}

func (w *writes) dndInfoCalls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dndReads
}

func (w *writes) Presence(context.Context) (domain.Presence, error) {
	return w.presence, w.record("users.getPresence")
}

func (w *writes) EndDND(context.Context) error { return w.record("dnd.endSnooze") }

func (w *writes) Users(_ context.Context, ids []domain.UserID) ([]domain.User, error) {
	return w.users, w.record("users.info", ids)
}

func (w *writes) Emoji(context.Context) (map[string]string, error) {
	return map[string]string{"shipit": "alias:squirrel"}, w.record("emoji.list")
}

func (w *writes) SendTyping(_ context.Context, conv domain.ConversationID) error {
	return w.record("typing", conv)
}

// Package providertest is a scripted provider.Provider for tests (task
// 1.9). A Fake replays real-time sessions (events, stream endings, Connect
// errors), serves history and threads from data the test sets, fails any
// method with errors queued in advance (RateLimitedError, ErrAuthExpired,
// anything) and records every call it receives.
package providertest

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// The method names a Fake records and fails by: the Slack Web API methods
// the real providers call, plus "connect" for the real-time stream.
const (
	MethodConnect    = "connect"
	MethodHistory    = "conversations.history"
	MethodReplies    = "conversations.replies"
	MethodPost       = "chat.postMessage"
	MethodMarkRead   = "conversations.mark"
	MethodReadMarker = "conversations.info"
)

// Session scripts one Connect call.
type Session struct {
	// Err makes Connect fail with it; the rest of the session is unused.
	Err error
	// Events are delivered in order on the stream.
	Events []domain.Event
	// Live, when not nil, is forwarded after Events until it closes. When
	// nil, the stream closes right after Events (a dropped connection).
	Live <-chan domain.Event
}

// Call is one call the Fake received. TS is the cursor that call took:
// History's before, Replies' and Post's thread, MarkRead's upTo.
type Call struct {
	Method string
	Conv   domain.ConversationID
	TS     domain.TS
}

type threadKey struct {
	conv   domain.ConversationID
	thread domain.TS
}

// Fake is a scripted provider. It is safe for concurrent use.
type Fake struct {
	team domain.TeamID
	caps domain.Capabilities

	mu       sync.Mutex
	sessions []Session
	history  map[domain.ConversationID][]domain.Message // newest first
	replies  map[threadKey][]domain.Message
	lastRead map[domain.ConversationID]domain.TS
	fails    map[string][]error
	calls    []Call
}

var _ provider.Provider = (*Fake)(nil)

// New builds a Fake bound to team that declares caps.
func New(team domain.TeamID, caps domain.Capabilities) *Fake {
	return &Fake{
		team:     team,
		caps:     caps,
		history:  make(map[domain.ConversationID][]domain.Message),
		replies:  make(map[threadKey][]domain.Message),
		lastRead: make(map[domain.ConversationID]domain.TS),
		fails:    make(map[string][]error),
	}
}

// Script queues sessions for the next Connect calls, in order. Once they
// run out, Connect opens a stream that stays silent until its ctx ends.
func (f *Fake) Script(sessions ...Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, sessions...)
}

// SetHistory replaces conv's history; msgs may come in any order.
func (f *Fake) SetHistory(conv domain.ConversationID, msgs ...domain.Message) {
	sorted := slices.SortedFunc(slices.Values(msgs), newestFirst)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history[conv] = sorted
}

// SetReplies replaces the replies of the thread rooted at thread.
func (f *Fake) SetReplies(conv domain.ConversationID, thread domain.TS, msgs ...domain.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[threadKey{conv, thread}] = slices.Clone(msgs)
}

// SetLastRead sets the read marker WithReadMarkers serves for conv.
func (f *Fake) SetLastRead(conv domain.ConversationID, ts domain.TS) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRead[conv] = ts
}

// Fail makes the next calls of method return errs, one per call, before
// the method works again.
func (f *Fake) Fail(method string, errs ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[method] = append(f.fails[method], errs...)
}

// Calls returns every call received so far, in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *Fake) Team() domain.TeamID { return f.team }

func (f *Fake) Capabilities() domain.Capabilities { return f.caps }

func (f *Fake) Connect(ctx context.Context) (<-chan domain.Event, error) {
	s := f.nextSession()
	if s.Err != nil {
		return nil, s.Err
	}
	out := make(chan domain.Event)
	go replay(ctx, s, out)
	return out, nil
}

func (f *Fake) History(_ context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record(Call{MethodHistory, conv, before}); err != nil {
		return nil, err
	}
	older := slices.DeleteFunc(slices.Clone(f.history[conv]), func(m domain.Message) bool {
		return !before.IsZero() && !m.TS.Before(before)
	})
	return older[:min(limit, len(older))], nil
}

func (f *Fake) Replies(_ context.Context, conv domain.ConversationID, thread domain.TS) ([]domain.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record(Call{MethodReplies, conv, thread}); err != nil {
		return nil, err
	}
	return slices.Clone(f.replies[threadKey{conv, thread}]), nil
}

func (f *Fake) Post(_ context.Context, conv domain.ConversationID, thread domain.TS, text string) (domain.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record(Call{MethodPost, conv, thread}); err != nil {
		return domain.Message{}, err
	}
	return domain.Message{ThreadTS: thread, Text: text}, nil
}

func (f *Fake) MarkRead(_ context.Context, conv domain.ConversationID, upTo domain.TS) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.record(Call{MethodMarkRead, conv, upTo})
}

// WithReadMarkers returns the same fake with a LastRead method, standing in
// for a provider that can read markers on demand but does not stream them.
func (f *Fake) WithReadMarkers() *ReadMarkerFake { return &ReadMarkerFake{f} }

// ReadMarkerFake is a Fake that can also read a conversation's marker.
type ReadMarkerFake struct{ *Fake }

// LastRead returns the marker set with SetLastRead.
func (r *ReadMarkerFake) LastRead(_ context.Context, conv domain.ConversationID) (domain.TS, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.record(Call{Method: MethodReadMarker, Conv: conv}); err != nil {
		return domain.TS{}, err
	}
	return r.lastRead[conv], nil
}

// nextSession pops the next scripted session, or an idle one.
func (f *Fake) nextSession() Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Call{Method: MethodConnect})
	if len(f.sessions) == 0 {
		return Session{Live: make(chan domain.Event)}
	}
	s := f.sessions[0]
	f.sessions = f.sessions[1:]
	return s
}

// record logs c and pops the next queued failure of its method. The
// caller holds mu.
func (f *Fake) record(c Call) error {
	f.calls = append(f.calls, c)
	errs := f.fails[c.Method]
	if len(errs) == 0 {
		return nil
	}
	f.fails[c.Method] = errs[1:]
	return errs[0]
}

// replay delivers a session's events, then its live feed, and closes out.
func replay(ctx context.Context, s Session, out chan<- domain.Event) {
	defer close(out)
	for _, ev := range s.Events {
		if !send(ctx, out, ev) {
			return
		}
	}
	forward(ctx, s.Live, out)
}

// forward copies live to out until live closes or ctx ends. A nil live
// returns at once.
func forward(ctx context.Context, live <-chan domain.Event, out chan<- domain.Event) {
	if live == nil {
		return
	}
	for {
		select {
		case ev, ok := <-live:
			if !ok || !send(ctx, out, ev) {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// send hands ev to out unless ctx has ended, and reports whether delivery
// may go on. ctx is checked first because a select with both cases ready
// picks one at random.
func send(ctx context.Context, out chan<- domain.Event, ev domain.Event) bool {
	if ctx.Err() == nil {
		select {
		case out <- ev:
		case <-ctx.Done():
		}
	}
	return ctx.Err() == nil
}

// newestFirst orders messages through their timestamps' fixed-width string
// form, which sorts like the timestamps themselves.
func newestFirst(a, b domain.Message) int {
	return cmp.Compare(b.TS.String(), a.TS.String())
}

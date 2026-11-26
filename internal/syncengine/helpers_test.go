package syncengine_test

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/ratebudget"
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/store/sqlite"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
)

// patience bounds every wait on the engine's goroutines, so a bug fails the
// test instead of hanging the suite. Nothing waits for it on success.
const patience = 5 * time.Second

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

var (
	zeroTS domain.TS
	team   = must(domain.ParseTeamID("T01"))
	convA  = must(domain.ParseConversationID("CAAA"))
	convB  = must(domain.ParseConversationID("CBBB"))
	dm     = must(domain.ParseConversationID("DDDD"))
	ts1    = must(domain.ParseTS("1700000001.000000"))
	ts2    = must(domain.ParseTS("1700000002.000000"))
	ts3    = must(domain.ParseTS("1700000003.000000"))
	ts4    = must(domain.ParseTS("1700000004.000000"))
)

func msg(at domain.TS) domain.Message { return domain.Message{TS: at, Text: "at " + at.String()} }

func created(c domain.ConversationID, at domain.TS) domain.Event {
	return domain.MessageCreated{Team: team, Conversation: c, Message: msg(at)}
}

func state(s domain.ConnectionState) domain.Event {
	return domain.ConnectionChanged{Team: team, State: s}
}

// recorder is the clients' side: it collects what the engine publishes.
type recorder struct{ ch chan domain.Event }

func newRecorder() *recorder { return &recorder{ch: make(chan domain.Event, 1000)} }

func (r *recorder) Publish(ev domain.Event) { r.ch <- ev }

// expect checks the next published events are exactly want, in order.
func (r *recorder) expect(t *testing.T, want ...domain.Event) {
	t.Helper()
	for i, w := range want {
		select {
		case got := <-r.ch:
			if got != w {
				t.Fatalf("published #%d = %#v, want %#v", i, got, w)
			}
		case <-time.After(patience):
			t.Fatalf("published #%d: nothing, want %#v", i, w)
		}
	}
}

// expectNothing checks nothing else was published so far.
func (r *recorder) expectNothing(t *testing.T) {
	t.Helper()
	select {
	case ev := <-r.ch:
		t.Fatalf("unexpected publish %#v", ev)
	default:
	}
}

// fakeClock hands every timer the engine creates to the test, which checks
// its delay and fires it. Time never passes on its own.
type fakeClock struct {
	now    time.Time
	timers chan *fakeTimer
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC), timers: make(chan *fakeTimer, 100)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) NewTimer(at time.Time) ratebudget.Timer {
	t := &fakeTimer{delay: at.Sub(c.now), c: make(chan time.Time, 1)}
	c.timers <- t
	return t
}

// fire waits for the engine's next timer, checks its delay and fires it.
func (c *fakeClock) fire(t *testing.T, delay time.Duration) {
	t.Helper()
	select {
	case tm := <-c.timers:
		if tm.delay != delay {
			t.Fatalf("timer delay = %v, want %v", tm.delay, delay)
		}
		tm.c <- c.now.Add(delay)
	case <-time.After(patience):
		t.Fatalf("no timer for %v", delay)
	}
}

func (c *fakeClock) expectNoTimer(t *testing.T) {
	t.Helper()
	select {
	case tm := <-c.timers:
		t.Fatalf("unexpected timer for %v", tm.delay)
	default:
	}
}

type fakeTimer struct {
	delay time.Duration
	c     chan time.Time
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }
func (t *fakeTimer) Stop()               {}

// budget grants every call at once and records what was asked of it.
type budget struct {
	mu        sync.Mutex
	waits     []grant
	penalties []penalty
}

type grant struct {
	method   string
	priority ratebudget.Priority
}

type penalty struct {
	method string
	after  time.Duration
}

func (b *budget) Wait(ctx context.Context, method string, p ratebudget.Priority) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.waits = append(b.waits, grant{method, p})
	return ctx.Err()
}

func (b *budget) Penalize(method string, retryAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.penalties = append(b.penalties, penalty{method, retryAfter})
}

func (b *budget) grants() []grant {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.waits)
}

func (b *budget) penalized() []penalty {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.penalties)
}

// rig is one engine with everything around it.
type rig struct {
	fake   *providertest.Fake
	store  store.Store
	pub    *recorder
	clock  *fakeClock
	budget *budget
	engine *syncengine.Engine
}

const pageSize = 3

func newRig(t *testing.T) *rig {
	t.Helper()
	return newRigWith(t, nil)
}

// newRigWith builds a rig whose engine talks to p, or to the rig's own
// fake when p is nil.
func newRigWith(t *testing.T, wrap func(*providertest.Fake) provider.Provider) *rig {
	t.Helper()
	st, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r := &rig{
		fake:   providertest.New(team, domain.NewCapabilities(domain.Realtime)),
		store:  st,
		pub:    newRecorder(),
		clock:  newFakeClock(),
		budget: &budget{},
	}
	var p provider.Provider = r.fake
	if wrap != nil {
		p = wrap(r.fake)
	}
	r.engine = syncengine.New(syncengine.Config{
		Provider:  p,
		Store:     st,
		Budget:    r.budget,
		Publisher: r.pub,
		Clock:     r.clock,
		Rand:      func() float64 { return 0.5 },
		PageSize:  pageSize,
	})
	return r
}

// start runs the engine until the test ends, and returns what Run
// returned once it does.
func (r *rig) start(t *testing.T) (cancel context.CancelFunc, result func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.engine.Run(ctx) }()
	stopped := false
	var err error
	result = func() error {
		t.Helper()
		if stopped {
			return err
		}
		select {
		case err = <-done:
			stopped = true
			return err
		case <-time.After(patience):
			t.Fatal("Run did not return")
			return nil
		}
	}
	t.Cleanup(func() {
		cancel()
		if !stopped {
			_ = result()
		}
	})
	return cancel, result
}

// stored returns conv's top-level messages in the store, newest first.
func (r *rig) stored(t *testing.T, c domain.ConversationID) []domain.TS {
	t.Helper()
	msgs, err := r.store.Messages(context.Background(), c, zeroTS, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.TS
	for _, m := range msgs {
		out = append(out, m.TS)
	}
	return out
}

// connected scripts a session that comes up and then stays open, and
// waits until the engine reports it.
func (r *rig) connected(t *testing.T) (cancel context.CancelFunc, result func() error) {
	t.Helper()
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: make(chan domain.Event)})
	cancel, result = r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected))
	return cancel, result
}

// cache puts msgs in the store as a previous session would have left them.
func (r *rig) cache(t *testing.T, c domain.ConversationID, msgs ...domain.Message) {
	t.Helper()
	if err := r.store.PutHistory(context.Background(), c, msgs); err != nil {
		t.Fatal(err)
	}
}

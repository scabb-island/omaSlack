package syncengine_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"reflect"
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
			if !reflect.DeepEqual(got, w) {
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

// fakeClock hands every timer the engine creates to the test, which finds
// it by its delay and fires it. Time passes only when the test advances
// it. Timers with other delays (a backoff next to a read poll) wait their
// turn.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers chan *fakeTimer
	held   []*fakeTimer // received but not fired; only the test touches it
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC), timers: make(chan *fakeTimer, 100)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// advance moves the clock forward by d without firing any timer.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *fakeClock) NewTimer(at time.Time) ratebudget.Timer {
	t := &fakeTimer{delay: at.Sub(c.Now()), c: make(chan time.Time, 1)}
	c.timers <- t
	return t
}

// fire waits for a timer with this delay and fires it.
func (c *fakeClock) fire(t *testing.T, delay time.Duration) {
	t.Helper()
	tm := c.take(t, delay)
	tm.c <- c.Now().Add(delay)
}

// await waits until the engine has created a timer with this delay,
// without firing it.
func (c *fakeClock) await(t *testing.T, delay time.Duration) {
	t.Helper()
	c.held = append(c.held, c.take(t, delay))
}

func (c *fakeClock) take(t *testing.T, delay time.Duration) *fakeTimer {
	t.Helper()
	deadline := time.After(patience)
	for {
		for i, tm := range c.held {
			if tm.delay == delay {
				c.held = slices.Delete(c.held, i, i+1)
				return tm
			}
		}
		select {
		case tm := <-c.timers:
			c.held = append(c.held, tm)
		case <-deadline:
			t.Fatalf("no timer for %v; pending: %v", delay, c.pending())
			return nil
		}
	}
}

func (c *fakeClock) pending() []time.Duration {
	var out []time.Duration
	for _, tm := range c.held {
		out = append(out, tm.delay)
	}
	return out
}

func (c *fakeClock) expectNoTimer(t *testing.T) {
	t.Helper()
	select {
	case tm := <-c.timers:
		t.Fatalf("unexpected timer for %v", tm.delay)
	default:
	}
	if len(c.held) > 0 {
		t.Fatalf("unexpected timers for %v", c.pending())
	}
}

type fakeTimer struct {
	delay time.Duration
	c     chan time.Time
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }
func (t *fakeTimer) Stop()               {}

// budget grants every call at once and records what was asked of it.
// One method can be held: its calls wait until their ctx ends, as under
// a long penalty, and each says so on held.
type budget struct {
	mu        sync.Mutex
	waits     []grant
	penalties []penalty
	hold      string
	held      chan string
	holding   int // calls of the held method waiting now
	mostHeld  int // the most that ever waited at once
}

// holdOn makes every call of method wait until its ctx ends.
func (b *budget) holdOn(method string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hold, b.held = method, make(chan string, 100)
}

// awaitHeld waits until a call of the held method is waiting.
func (b *budget) awaitHeld(t *testing.T) {
	t.Helper()
	select {
	case <-b.held:
	case <-time.After(patience):
		t.Fatal("no call is waiting on the budget")
	}
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
	b.waits = append(b.waits, grant{method, p})
	held := method == b.hold
	if held {
		b.holding++
		b.mostHeld = max(b.mostHeld, b.holding)
	}
	b.mu.Unlock()
	if held {
		b.held <- method
		<-ctx.Done()
		b.mu.Lock()
		b.holding--
		b.mu.Unlock()
	}
	return ctx.Err()
}

// mostHeldAtOnce is how many calls of the held method ever waited at once.
func (b *budget) mostHeldAtOnce() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mostHeld
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
	logs   *lockedBuffer
	engine *syncengine.Engine
	stop   context.CancelFunc // ends Run, once start ran
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
	return newRigFrom(t, wrap, func(s store.Store) store.Store { return s })
}

// newRigFrom also lets the test stand between the engine and the store.
func newRigFrom(t *testing.T, wrap func(*providertest.Fake) provider.Provider, wrapStore func(store.Store) store.Store) *rig {
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
		logs:   &lockedBuffer{},
	}
	var p provider.Provider = r.fake
	if wrap != nil {
		p = wrap(r.fake)
	}
	r.engine = syncengine.New(syncengine.Config{
		Provider:  p,
		Store:     wrapStore(st),
		Budget:    r.budget,
		Publisher: r.pub,
		Self:      me,
		Clock:     r.clock,
		Rand:      func() float64 { return 0.5 },
		PageSize:  pageSize,
		Log:       slog.New(slog.NewTextHandler(r.logs, nil)),

		MaxDownload: fileLimit,
		MaxUpload:   fileLimit,
	})
	return r
}

// start runs the engine until the test ends, and returns what Run
// returned once it does.
func (r *rig) start(t *testing.T) (cancel context.CancelFunc, result func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.stop = cancel
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
// waits until the engine reports it and has asked for its conversation
// list.
func (r *rig) connected(t *testing.T) (cancel context.CancelFunc, result func() error) {
	t.Helper()
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: make(chan domain.Event)})
	cancel, result = r.start(t)
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected))
	r.awaitListed(t, 1)
	return cancel, result
}

// cache puts msgs in the store as a previous session would have left them.
func (r *rig) cache(t *testing.T, c domain.ConversationID, msgs ...domain.Message) {
	t.Helper()
	if err := r.store.PutHistory(context.Background(), c, msgs); err != nil {
		t.Fatal(err)
	}
}

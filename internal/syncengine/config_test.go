package syncengine_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/store/sqlite"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
)

// failingStore rejects the first message it is asked to apply.
type failingStore struct {
	store.Store
	once sync.Once
}

func (s *failingStore) Apply(ctx context.Context, ev domain.Event) error {
	failed := false
	s.once.Do(func() { failed = true })
	if failed {
		return errors.New("disk full")
	}
	return s.Store.Apply(ctx, ev)
}

// lockedBuffer is a log sink the engine's goroutines can write to while
// the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestAnEventTheStoreRejectsIsLoggedAndNotPublished(t *testing.T) {
	st, err := sqlite.Open(bg, filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fake := providertest.New(team, domain.Capabilities{})
	live := make(chan domain.Event)
	fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: live})
	pub := newRecorder()
	logs := &lockedBuffer{}
	e := syncengine.New(syncengine.Config{
		Provider:  fake.WithReadMarkers(), // exercises the default poll interval
		Store:     &failingStore{Store: st},
		Budget:    &budget{},
		Publisher: pub,
		Log:       slog.New(slog.NewTextHandler(logs, nil)),
	})
	ctx, cancel := context.WithCancel(bg)
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()

	pub.expect(t, state(domain.Connecting), state(domain.Connected))
	live <- created(convA, ts1)
	live <- created(convA, ts2)
	pub.expect(t, created(convA, ts2))
	cancel()
	<-done

	if !strings.Contains(logs.String(), "disk full") {
		t.Fatalf("log = %q, want the store error", logs.String())
	}
}

func TestAnEngineWithOnlyTheRequiredFieldsRuns(t *testing.T) {
	st, err := sqlite.Open(bg, filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fake := providertest.New(team, domain.Capabilities{})
	fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected), created(convA, ts1)}})
	fake.SetHistory(convA, msg(ts1), msg(ts2))
	pub := newRecorder()
	e := syncengine.New(syncengine.Config{Provider: fake, Store: st, Budget: &budget{}, Publisher: pub})
	ctx, cancel := context.WithCancel(bg)
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()

	// The stream drops and the engine waits out its backoff on the wall
	// clock; the test does not wait for it.
	pub.expect(t, state(domain.Connecting), state(domain.Connected), created(convA, ts1), state(domain.Disconnected))
	if err := e.OpenConversation(ctx, convA); err != nil {
		t.Fatal(err)
	}
	pub.expect(t, created(convA, ts2))
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
}

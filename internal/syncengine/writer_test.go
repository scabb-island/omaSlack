package syncengine_test

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// exclusiveStore counts writes that overlap another write. With a single
// writer there are none, however many goroutines use the engine.
type exclusiveStore struct {
	store.Store
	writing  atomic.Int32
	overlaps atomic.Int32
}

func (s *exclusiveStore) enter() func() {
	if s.writing.Add(1) > 1 {
		s.overlaps.Add(1)
	}
	runtime.Gosched() // give a second writer every chance to show up
	return func() { s.writing.Add(-1) }
}

func (s *exclusiveStore) Apply(ctx context.Context, ev domain.Event) error {
	defer s.enter()()
	return s.Store.Apply(ctx, ev)
}

func (s *exclusiveStore) PutHistory(ctx context.Context, c domain.ConversationID, msgs []domain.Message) error {
	defer s.enter()()
	return s.Store.PutHistory(ctx, c, msgs)
}

// tsAt is the timestamp of the i-th test message.
func tsAt(i int) domain.TS { return must(domain.ParseTS(fmt.Sprintf("17000%05d.000000", i))) }

func TestTheEngineIsTheStoresOnlyWriter(t *testing.T) {
	ex := &exclusiveStore{}
	r := newRigFrom(t, nil, func(s store.Store) store.Store { ex.Store = s; return ex })
	const events, clients, rounds = 200, 4, 10
	var history []domain.Message
	for i := range events {
		history = append(history, msg(tsAt(events+i)))
	}
	r.fake.SetHistory(convB, history...)
	r.fake.SetReplies(convB, tsAt(events), msg(tsAt(2*events)))
	live := make(chan domain.Event)
	r.fake.Script(providertest.Session{Events: []domain.Event{state(domain.Connected)}, Live: live})
	r.start(t)

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range events {
			live <- created(convA, tsAt(i))
		}
	})
	for range clients {
		wg.Go(func() {
			for range rounds {
				_ = r.engine.OpenConversation(bg, convB)
				_, _ = r.engine.LoadOlder(bg, convB)
				_ = r.engine.OpenThread(bg, convB, tsAt(events))
				_ = r.engine.CloseConversation(bg, convB)
			}
		})
	}
	wg.Wait()
	r.pub.expect(t, state(domain.Connecting), state(domain.Connected))
	for i := range events {
		r.pub.expect(t, created(convA, tsAt(i)))
	}

	if n := ex.overlaps.Load(); n != 0 {
		t.Fatalf("%d writes overlapped another", n)
	}
	if got := r.stored(t, convA); len(got) != min(events, 100) {
		t.Fatalf("stored %d live messages", len(got))
	}
}

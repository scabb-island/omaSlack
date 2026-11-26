package sqlite_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// TestReadersRunAlongsideTheWriter has one writer storing history pages
// and events while several readers query the store. No call may fail
// (WAL lets readers proceed during a write), and a reader must never see
// half a page, because each page is one transaction.
func TestReadersRunAlongsideTheWriter(t *testing.T) {
	const pages, pageSize, readers = 40, 5, 4
	ctx := context.Background()
	s := openStore(t)
	c := conv(t, "C01")

	var wg sync.WaitGroup
	done := make(chan struct{})
	errs := make(chan error, readers+1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		errs <- writePages(ctx, s, c, pages, pageSize)
	}()
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- readUntil(ctx, s, c, pageSize, done)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}

	if got := len(messages(t, s, "C01", zeroTS, pages*pageSize+1)); got != pages*pageSize {
		t.Errorf("stored %d messages, want %d", got, pages*pageSize)
	}
	if got := lastRead(t, s, "C01"); got != pageTS(pages-1, pageSize-1) {
		t.Errorf("LastRead = %v, want the newest message", got)
	}
}

func pageTS(page, i int) domain.TS {
	ts, err := domain.ParseTS(fmt.Sprintf("17%08d.%06d", page, i))
	if err != nil {
		panic(err)
	}
	return ts
}

func writePages(ctx context.Context, s store.Store, c domain.ConversationID, pages, pageSize int) error {
	for p := range pages {
		page := make([]domain.Message, pageSize)
		for i := range page {
			page[i] = domain.Message{TS: pageTS(p, i), Text: "m"}
		}
		if err := s.PutHistory(ctx, c, page); err != nil {
			return fmt.Errorf("PutHistory: %w", err)
		}
		if err := s.Apply(ctx, domain.ReadChanged{Conversation: c, LastRead: pageTS(p, pageSize-1)}); err != nil {
			return fmt.Errorf("Apply: %w", err)
		}
	}
	return nil
}

func readUntil(ctx context.Context, s store.Store, c domain.ConversationID, pageSize int, done <-chan struct{}) error {
	for {
		select {
		case <-done:
			return nil
		default:
		}
		got, err := s.Messages(ctx, c, domain.TS{}, 1000)
		if err != nil {
			return fmt.Errorf("Messages: %w", err)
		}
		if len(got)%pageSize != 0 {
			return fmt.Errorf("saw %d messages: half a page", len(got))
		}
		if _, err := s.LastRead(ctx, c); err != nil {
			return fmt.Errorf("LastRead: %w", err)
		}
	}
}

func TestAClosedStoreReportsErrors(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	c, at := conv(t, "C01"), ts(t, "1700000001.000000")
	m := domain.Message{TS: at}

	for name, call := range map[string]func() error{
		"Apply created":  func() error { return s.Apply(ctx, domain.MessageCreated{Conversation: c, Message: m}) },
		"Apply deleted":  func() error { return s.Apply(ctx, domain.MessageDeleted{Conversation: c, TS: at}) },
		"Apply reaction": func() error { return s.Apply(ctx, domain.ReactionChanged{Conversation: c, TS: at, Added: true}) },
		"Apply read":     func() error { return s.Apply(ctx, domain.ReadChanged{Conversation: c, LastRead: at}) },
		"PutHistory":     func() error { return s.PutHistory(ctx, c, []domain.Message{m}) },
		"SetDraft":       func() error { return s.SetDraft(ctx, c, at, "x") },
		"Messages":       func() error { _, err := s.Messages(ctx, c, at, 1); return err },
		"Reactions":      func() error { _, err := s.Reactions(ctx, c, at); return err },
		"LastRead":       func() error { _, err := s.LastRead(ctx, c); return err },
		"Draft":          func() error { _, err := s.Draft(ctx, c, at); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s on a closed store succeeded", name)
		}
	}
}

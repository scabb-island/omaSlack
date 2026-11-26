package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

func mustPutConversations(t *testing.T, s store.Store, list ...domain.Conversation) {
	t.Helper()
	if err := s.PutConversations(context.Background(), list); err != nil {
		t.Fatalf("PutConversations: %v", err)
	}
}

func conversations(t *testing.T, s store.Store) []domain.Conversation {
	t.Helper()
	got, err := s.Conversations(context.Background())
	if err != nil {
		t.Fatalf("Conversations: %v", err)
	}
	return got
}

func expectConversations(t *testing.T, s store.Store, want ...domain.Conversation) {
	t.Helper()
	if got := conversations(t, s); !reflect.DeepEqual(got, want) {
		t.Errorf("Conversations =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestNoListIsEmpty(t *testing.T) {
	if got := conversations(t, openStore(t)); len(got) != 0 {
		t.Errorf("Conversations = %+v, want none", got)
	}
}

func TestTheListIsStoredOrderedByID(t *testing.T) {
	s := openStore(t)
	general := domain.Conversation{ID: conv(t, "C02"), Kind: domain.Channel, Name: "general", Latest: ts(t, "1700000002.000000"), LastRead: ts(t, "1700000001.000000"), Mentions: 2}
	secret := domain.Conversation{ID: conv(t, "G01"), Kind: domain.PrivateChannel, Name: "secret"}
	direct := domain.Conversation{ID: conv(t, "D01"), Kind: domain.DirectMessage, Latest: ts(t, "1700000003.000000")}
	group := domain.Conversation{ID: conv(t, "C01"), Kind: domain.GroupMessage, Name: "mpdm-a--b"}
	mustPutConversations(t, s, general, secret, direct, group)
	expectConversations(t, s, group, general, direct, secret)
}

func TestPuttingTheSameListTwiceChangesNothing(t *testing.T) {
	s := openStore(t)
	c := domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel, Name: "a", Latest: ts(t, "1700000002.000000"), Mentions: 1}
	mustPutConversations(t, s, c)
	mustPutConversations(t, s, c)
	expectConversations(t, s, c)
}

func TestANewListReplacesTheOld(t *testing.T) {
	s := openStore(t)
	left := domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel, Name: "left"}
	renamed := domain.Conversation{ID: conv(t, "C02"), Kind: domain.Channel, Name: "old name"}
	mustPutConversations(t, s, left, renamed)
	renamed.Name = "new name"
	mustPutConversations(t, s, renamed)
	expectConversations(t, s, renamed)
}

func TestTheListsMarkersOnlyMoveForward(t *testing.T) {
	s := openStore(t)
	mustApply(t, s, readTo(t, "C01", "1700000003.000000"))
	mustPutConversations(t, s,
		domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel, LastRead: ts(t, "1700000002.000000")},
		domain.Conversation{ID: conv(t, "C02"), Kind: domain.Channel, LastRead: ts(t, "1700000002.000000")},
	)
	if got := lastRead(t, s, "C01"); got != ts(t, "1700000003.000000") {
		t.Errorf("C01 marker = %v, want the newer one kept", got)
	}
	if got := lastRead(t, s, "C02"); got != ts(t, "1700000002.000000") {
		t.Errorf("C02 marker = %v, want the list's", got)
	}
}

func TestAListWithoutAMarkerKeepsTheStoredOne(t *testing.T) {
	s := openStore(t)
	mustApply(t, s, readTo(t, "C01", "1700000003.000000"))
	mustPutConversations(t, s, domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel})
	expectConversations(t, s, domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel, LastRead: ts(t, "1700000003.000000")})
}

func TestAMessageNewerThanTheListIsTheLatest(t *testing.T) {
	s := openStore(t)
	c := domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel, Latest: ts(t, "1700000002.000000"), LastRead: ts(t, "1700000002.000000")}
	mustPutConversations(t, s, c)
	mustApply(t, s, created(t, "C01", msg(t, "1700000003.000000", "news")))

	c.Latest = ts(t, "1700000003.000000")
	expectConversations(t, s, c)
	if !conversations(t, s)[0].Unread() {
		t.Error("a message after the marker is not unread")
	}
}

func TestOnlyLiveTopLevelMessagesMoveTheLatest(t *testing.T) {
	s := openStore(t)
	latest := ts(t, "1700000002.000000")
	c := domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel, Latest: latest}
	mustPutConversations(t, s, c)
	reply := msg(t, "1700000004.000000", "reply")
	reply.ThreadTS = ts(t, "1700000001.000000")
	mustApply(t, s,
		created(t, "C01", msg(t, "1700000001.000000", "older")),
		created(t, "C01", reply),
		created(t, "C01", msg(t, "1700000003.000000", "gone")),
		deleted(t, "C01", "1700000003.000000"),
		created(t, "C02", msg(t, "1700000005.000000", "elsewhere")),
	)
	expectConversations(t, s, c)
}

func TestMentionsDropOnceTheMarkerReachesTheLatest(t *testing.T) {
	s := openStore(t)
	c := domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel, Latest: ts(t, "1700000002.000000"), Mentions: 3}
	mustPutConversations(t, s, c)
	expectConversations(t, s, c)

	mustApply(t, s, readTo(t, "C01", "1700000002.000000"))
	c.LastRead, c.Mentions = ts(t, "1700000002.000000"), 0
	expectConversations(t, s, c)
}

func TestMentionsStandWhileTheLatestIsUnknown(t *testing.T) {
	s := openStore(t)
	mustApply(t, s, readTo(t, "C01", "1700000002.000000"))
	c := domain.Conversation{ID: conv(t, "C01"), Kind: domain.Channel, Mentions: 1}
	mustPutConversations(t, s, c)
	c.LastRead = ts(t, "1700000002.000000")
	expectConversations(t, s, c)
}

func TestTheListSurvivesAReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "T123.db")
	c := domain.Conversation{ID: conv(t, "D01"), Kind: domain.DirectMessage, Latest: ts(t, "1700000002.000000"), Mentions: 1}
	s := openAt(t, path)
	mustPutConversations(t, s, c)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := openAt(t, path).Conversations(ctx)
	if err != nil || !reflect.DeepEqual(got, []domain.Conversation{c}) {
		t.Errorf("Conversations after reopen = %+v, %v", got, err)
	}
}

func TestACorruptListRowIsAnError(t *testing.T) {
	for name, stmt := range map[string]string{
		"kind":   `INSERT INTO conversations (conv_id, kind) VALUES ('C01', 'forum')`,
		"id":     `INSERT INTO conversations (conv_id, kind) VALUES ('x', 'channel')`,
		"latest": `INSERT INTO conversations (conv_id, kind, latest) VALUES ('C01', 'channel', 'soon')`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "T123.db")
			s := openAt(t, path)
			rawExec(t, path, stmt)
			if _, err := s.Conversations(context.Background()); err == nil {
				t.Error("Conversations read a corrupt row")
			}
		})
	}
}

func TestAClosedStoreCannotListConversations(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.PutConversations(ctx, []domain.Conversation{{ID: conv(t, "C01"), Kind: domain.Channel}}); err == nil {
		t.Error("PutConversations on a closed store succeeded")
	}
	if _, err := s.Conversations(ctx); err == nil {
		t.Error("Conversations on a closed store succeeded")
	}
}

// TestTheListIsReadAlongsideItsWriter has one writer replacing the list
// while readers read it. No call may fail, and a reader must never see an
// empty list: each replacement is one transaction.
func TestTheListIsReadAlongsideItsWriter(t *testing.T) {
	const rounds, size, readers = 30, 8, 4
	ctx := context.Background()
	s := openStore(t)
	list := make([]domain.Conversation, size)
	for i := range list {
		list[i] = domain.Conversation{ID: conv(t, "C0"+string(rune('A'+i))), Kind: domain.Channel}
	}

	mustPutConversations(t, s, list...)

	var wg sync.WaitGroup
	done := make(chan struct{})
	errs := make(chan error, readers+1)
	wg.Go(func() {
		defer close(done)
		for i := range rounds {
			if err := s.PutConversations(ctx, list[:1+i%size]); err != nil {
				errs <- err
				return
			}
		}
	})
	for range readers {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				got, err := s.Conversations(ctx)
				if err != nil {
					errs <- err
					return
				}
				if len(got) == 0 {
					errs <- errors.New("saw an empty list: half a replacement")
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := len(conversations(t, s)); got != 1+(rounds-1)%size {
		t.Errorf("%d conversations, want the last list's %d", got, 1+(rounds-1)%size)
	}
}

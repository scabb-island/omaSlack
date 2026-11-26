package sqlite_test

import (
	"context"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

func reply(t *testing.T, at, root, text string) domain.Message {
	t.Helper()
	m := msg(t, at, text)
	m.ThreadTS = ts(t, root)
	return m
}

func replies(t *testing.T, s store.Store, c, thread string) []domain.Message {
	t.Helper()
	got, err := s.Replies(context.Background(), conv(t, c), ts(t, thread))
	if err != nil {
		t.Fatalf("Replies: %v", err)
	}
	return got
}

// A channel view shows top-level messages and thread roots; the replies
// live behind the root.
func TestMessagesLeaveThreadRepliesOut(t *testing.T) {
	s := openStore(t)
	root := msg(t, "1700000001.000100", "root")
	root.ThreadTS = root.TS
	mustPutHistory(t, s, "C01",
		root,
		reply(t, "1700000002.000200", "1700000001.000100", "first reply"),
		msg(t, "1700000003.000300", "top level"),
		reply(t, "1700000004.000400", "1700000001.000100", "second reply"),
	)
	wantTexts(t, messages(t, s, "C01", zeroTS, 10), "top level", "root")
}

func TestRepliesAreOldestFirstWithoutTheRoot(t *testing.T) {
	s := openStore(t)
	root := msg(t, "1700000001.000100", "root")
	root.ThreadTS = root.TS
	mustPutHistory(t, s, "C01",
		reply(t, "1700000004.000400", "1700000001.000100", "second reply"),
		root,
		reply(t, "1700000002.000200", "1700000001.000100", "first reply"),
	)
	wantTexts(t, replies(t, s, "C01", "1700000001.000100"), "first reply", "second reply")
}

func TestRepliesAreScopedToTheirThreadAndConversation(t *testing.T) {
	s := openStore(t)
	mustPutHistory(t, s, "C01",
		reply(t, "1700000002.000200", "1700000001.000100", "in thread A"),
		reply(t, "1700000003.000300", "1700000001.000999", "in thread B"),
	)
	mustPutHistory(t, s, "C02", reply(t, "1700000004.000400", "1700000001.000100", "other channel"))

	wantTexts(t, replies(t, s, "C01", "1700000001.000100"), "in thread A")
	wantTexts(t, replies(t, s, "C01", "1700000009.000900")) // no such thread
}

func TestRepliesSkipDeletedMessages(t *testing.T) {
	s := openStore(t)
	gone := reply(t, "1700000002.000200", "1700000001.000100", "gone")
	mustPutHistory(t, s, "C01", gone, reply(t, "1700000003.000300", "1700000001.000100", "kept"))
	if err := s.Apply(context.Background(), domain.MessageDeleted{Conversation: conv(t, "C01"), TS: gone.TS}); err != nil {
		t.Fatal(err)
	}
	wantTexts(t, replies(t, s, "C01", "1700000001.000100"), "kept")
}

package sqlite_test

import (
	"context"
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// threadRoot is a root at 1700000001.000100 with count replies, the
// newest at latest ("" for unknown).
func threadRoot(t *testing.T, count int, latest string) domain.Message {
	t.Helper()
	m := msg(t, "1700000001.000100", "root")
	m.ThreadTS = m.TS
	m.ReplyCount = count
	if latest != "" {
		m.LatestReply = ts(t, latest)
	}
	return m
}

func onlyMessage(t *testing.T, s store.Store, c string) domain.Message {
	t.Helper()
	got := messages(t, s, c, zeroTS, 10)
	if len(got) != 1 {
		t.Fatalf("Messages = %+v, want one", got)
	}
	return got[0]
}

func TestThreadRootsKeepTheirReplyCount(t *testing.T) {
	s := openStore(t)
	root := threadRoot(t, 3, "1700000005.000500")
	mustPutHistory(t, s, "C01", root)

	if got := onlyMessage(t, s, "C01"); !reflect.DeepEqual(got, root) {
		t.Errorf("Messages = %+v, want %+v", got, root)
	}
}

// An edit of the root (or a copy from a source that does not report
// replies) says nothing of them: what the store knows is kept. A copy
// that does report them replaces it.
func TestAnUpdateWithoutReplyCountKeepsTheStoredOne(t *testing.T) {
	s := openStore(t)
	mustPutHistory(t, s, "C01", threadRoot(t, 3, "1700000005.000500"))

	edit := threadRoot(t, 0, "")
	edit.Text, edit.Edited = "root, edited", ts(t, "1700000009.000000")
	mustApply(t, s, updated(t, "C01", edit))
	want := threadRoot(t, 3, "1700000005.000500")
	want.Text, want.Edited = edit.Text, edit.Edited
	if got := onlyMessage(t, s, "C01"); !reflect.DeepEqual(got, want) {
		t.Errorf("after an edit: %+v, want %+v", got, want)
	}

	fresh := threadRoot(t, 4, "1700000006.000600")
	fresh.Text, fresh.Edited = want.Text, want.Edited
	mustPutHistory(t, s, "C01", fresh)
	if got := onlyMessage(t, s, "C01"); !reflect.DeepEqual(got, fresh) {
		t.Errorf("after a fresh copy: %+v, want %+v", got, fresh)
	}
}

func countReply(t *testing.T, s store.Store, r domain.Message) (domain.Message, bool) {
	t.Helper()
	root, ok, err := s.CountReply(context.Background(), conv(t, "C01"), r)
	if err != nil {
		t.Fatalf("CountReply: %v", err)
	}
	return root, ok
}

func TestCountReplyCountsEachNewReplyOnce(t *testing.T) {
	s := openStore(t)
	mustPutHistory(t, s, "C01", threadRoot(t, 3, "1700000005.000500"))

	r := reply(t, "1700000007.000700", "1700000001.000100", "new")
	root, ok := countReply(t, s, r)
	if want := threadRoot(t, 4, "1700000007.000700"); !ok || !reflect.DeepEqual(root, want) {
		t.Fatalf("CountReply = %+v, %v; want %+v, true", root, ok, want)
	}
	// Slack echoes the user's own reply after chat.postMessage returned:
	// the second copy is not a second reply.
	if _, ok := countReply(t, s, r); ok {
		t.Error("the same reply was counted twice")
	}
	if _, ok := countReply(t, s, reply(t, "1700000004.000400", "1700000001.000100", "older")); ok {
		t.Error("a reply older than the newest known one was counted")
	}
	if got := onlyMessage(t, s, "C01"); !reflect.DeepEqual(got, threadRoot(t, 4, "1700000007.000700")) {
		t.Errorf("stored root = %+v", got)
	}
}

// With no replies known, the first reply counts as the first.
func TestCountReplyOnARootWithoutReplies(t *testing.T) {
	s := openStore(t)
	mustPutHistory(t, s, "C01", threadRoot(t, 0, ""))

	root, ok := countReply(t, s, reply(t, "1700000002.000200", "1700000001.000100", "first"))
	if want := threadRoot(t, 1, "1700000002.000200"); !ok || !reflect.DeepEqual(root, want) {
		t.Errorf("CountReply = %+v, %v; want %+v, true", root, ok, want)
	}
}

func TestCountReplyWithoutACachedRoot(t *testing.T) {
	s := openStore(t)
	r := reply(t, "1700000002.000200", "1700000001.000100", "orphan")
	if _, ok := countReply(t, s, r); ok {
		t.Error("counted a reply whose root is not cached")
	}
	mustPutHistory(t, s, "C01", threadRoot(t, 1, ""))
	mustApply(t, s, deleted(t, "C01", "1700000001.000100"))
	if _, ok := countReply(t, s, r); ok {
		t.Error("counted a reply of a deleted root")
	}
	if _, ok := countReply(t, s, msg(t, "1700000003.000300", "top level")); ok {
		t.Error("counted a message that is not a reply")
	}
}

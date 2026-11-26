package sqlite_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

func mustApply(t *testing.T, s store.Store, events ...domain.Event) {
	t.Helper()
	for _, ev := range events {
		if err := s.Apply(context.Background(), ev); err != nil {
			t.Fatalf("Apply(%s): %v", ev.Topic(), err)
		}
	}
}

func created(t *testing.T, c string, m domain.Message) domain.Event {
	t.Helper()
	return domain.MessageCreated{Conversation: conv(t, c), Message: m}
}

func updated(t *testing.T, c string, m domain.Message) domain.Event {
	t.Helper()
	return domain.MessageUpdated{Conversation: conv(t, c), Message: m}
}

func deleted(t *testing.T, c, at string) domain.Event {
	t.Helper()
	return domain.MessageDeleted{Conversation: conv(t, c), TS: ts(t, at)}
}

func readTo(t *testing.T, c, at string) domain.Event {
	t.Helper()
	return domain.ReadChanged{Conversation: conv(t, c), LastRead: ts(t, at)}
}

func lastRead(t *testing.T, s store.Store, c string) domain.TS {
	t.Helper()
	got, err := s.LastRead(context.Background(), conv(t, c))
	if err != nil {
		t.Fatalf("LastRead: %v", err)
	}
	return got
}

func editedMsg(t *testing.T, at, text, editedAt string) domain.Message {
	t.Helper()
	m := msg(t, at, text)
	m.Edited = ts(t, editedAt)
	return m
}

func TestMessageCreatedIsStoredOnce(t *testing.T) {
	s := openStore(t)
	ev := created(t, "C01", msg(t, "1700000001.000000", "hi"))
	mustApply(t, s, ev, ev)

	got := messages(t, s, "C01", zeroTS, 10)
	if want := []domain.Message{msg(t, "1700000001.000000", "hi")}; !reflect.DeepEqual(got, want) {
		t.Errorf("Messages = %+v, want %+v", got, want)
	}
}

func TestMessageUpdatedReplacesTheText(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		created(t, "C01", msg(t, "1700000001.000000", "typo")),
		updated(t, "C01", editedMsg(t, "1700000001.000000", "fixed", "1700000005.000000")),
	)
	wantTexts(t, messages(t, s, "C01", zeroTS, 10), "fixed")
}

func TestMessageUpdatedForAnUnseenMessageStoresIt(t *testing.T) {
	s := openStore(t)
	mustApply(t, s, updated(t, "C01", editedMsg(t, "1600000000.000000", "old but edited", "1700000005.000000")))
	wantTexts(t, messages(t, s, "C01", zeroTS, 10), "old but edited")
}

func TestLateCreateDoesNotUndoAnEdit(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		updated(t, "C01", editedMsg(t, "1700000001.000000", "second edit", "1700000009.000000")),
		updated(t, "C01", editedMsg(t, "1700000001.000000", "first edit", "1700000005.000000")),
		created(t, "C01", msg(t, "1700000001.000000", "original")),
	)
	wantTexts(t, messages(t, s, "C01", zeroTS, 10), "second edit")
}

func TestMessageDeletedHidesItForGood(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		created(t, "C01", msg(t, "1700000001.000000", "keep")),
		created(t, "C01", msg(t, "1700000002.000000", "oops")),
		deleted(t, "C01", "1700000002.000000"),
		deleted(t, "C01", "1700000002.000000"),
		// Events that arrive after the delete cannot bring it back.
		created(t, "C01", msg(t, "1700000002.000000", "oops")),
		updated(t, "C01", editedMsg(t, "1700000002.000000", "oops edited", "1700000009.000000")),
	)
	mustPutHistory(t, s, "C01", msg(t, "1700000002.000000", "oops"))

	wantTexts(t, messages(t, s, "C01", zeroTS, 10), "keep")
}

func TestDeleteBeforeCreateStillHidesIt(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		deleted(t, "C01", "1700000002.000000"),
		created(t, "C01", msg(t, "1700000002.000000", "ghost")),
	)
	wantTexts(t, messages(t, s, "C01", zeroTS, 10))
}

func TestNoReadMarkerIsZero(t *testing.T) {
	if got := lastRead(t, openStore(t), "C01"); !got.IsZero() {
		t.Errorf("LastRead = %v, want zero", got)
	}
}

func TestReadMarkerMovesForward(t *testing.T) {
	s := openStore(t)
	mustApply(t, s, readTo(t, "C01", "1700000001.000000"), readTo(t, "C01", "1700000003.000000"))
	if got, want := lastRead(t, s, "C01"), ts(t, "1700000003.000000"); got != want {
		t.Errorf("LastRead = %v, want %v", got, want)
	}
}

// A read marker never moves backwards: events can arrive out of order
// (a reconnect replays an older read.changed after a newer one), and an
// older marker would wrongly mark messages unread again.
func TestReadMarkerNeverMovesBackwards(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		readTo(t, "C01", "1700000003.000000"),
		readTo(t, "C01", "1700000001.000000"),
		readTo(t, "C01", "1700000003.000000"),
	)
	if got, want := lastRead(t, s, "C01"), ts(t, "1700000003.000000"); got != want {
		t.Errorf("LastRead = %v, want %v", got, want)
	}
}

func TestReadMarkersArePerConversation(t *testing.T) {
	s := openStore(t)
	mustApply(t, s, readTo(t, "C01", "1700000003.000000"))
	if got := lastRead(t, s, "D02"); !got.IsZero() {
		t.Errorf("LastRead(D02) = %v, want zero", got)
	}
}

func TestCorruptReadMarkerIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "T123.db")
	s := openAt(t, path)
	rawExec(t, path, `INSERT INTO read_markers (conv_id, last_read) VALUES ('C01', 'soon')`)
	if _, err := s.LastRead(context.Background(), conv(t, "C01")); err == nil {
		t.Error("LastRead accepted a corrupt marker")
	}
}

func TestEventsWithoutStateAreAcceptedNoOps(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		domain.PresenceChanged{User: user(t, "U01"), Presence: domain.Active},
		domain.UserTyping{Conversation: conv(t, "C01"), User: user(t, "U01")},
		domain.ConnectionChanged{State: domain.Connected},
	)
	wantTexts(t, messages(t, s, "C01", zeroTS, 10))
	if got := lastRead(t, s, "C01"); !got.IsZero() {
		t.Errorf("LastRead = %v, want zero", got)
	}
}

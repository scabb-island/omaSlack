package sqlite_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// msg builds a plain message by user U01 with the given timestamp.
func msg(t *testing.T, at, text string) domain.Message {
	t.Helper()
	return domain.Message{TS: ts(t, at), Author: author(t, "U01"), Text: text}
}

func mustPutHistory(t *testing.T, s store.Store, c string, msgs ...domain.Message) {
	t.Helper()
	if err := s.PutHistory(context.Background(), conv(t, c), msgs); err != nil {
		t.Fatalf("PutHistory: %v", err)
	}
}

func messages(t *testing.T, s store.Store, c string, before domain.TS, limit int) []domain.Message {
	t.Helper()
	got, err := s.Messages(context.Background(), conv(t, c), before, limit)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	return got
}

// texts lists the texts of msgs, which is enough to check order and
// membership.
func texts(msgs []domain.Message) []string {
	out := []string{}
	for _, m := range msgs {
		out = append(out, m.Text)
	}
	return out
}

func wantTexts(t *testing.T, got []domain.Message, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if g := texts(got); !reflect.DeepEqual(g, want) {
		t.Errorf("messages = %q, want %q", g, want)
	}
}

func TestNoMessages(t *testing.T) {
	wantTexts(t, messages(t, openStore(t), "C01", zeroTS, 10))
}

func TestMessagesRoundTripEveryField(t *testing.T) {
	s := openStore(t)
	full := domain.Message{
		TS:       ts(t, "1700000002.000200"),
		ThreadTS: ts(t, "1700000001.000100"),
		Author:   author(t, "W02"),
		Text:     "héllo 👋 *bold*", // non-ASCII on purpose: text must round-trip byte for byte
		Edited:   ts(t, "1700000003.000300"),
	}
	bare := domain.Message{TS: ts(t, "1700000001.000100")} // no thread, author or edit
	mustPutHistory(t, s, "C01", full, bare)

	// full is a reply, so it comes back through Replies; bare is top-level.
	if got := messages(t, s, "C01", zeroTS, 10); !reflect.DeepEqual(got, []domain.Message{bare}) {
		t.Errorf("Messages = %+v, want [%+v]", got, bare)
	}
	if got := replies(t, s, "C01", "1700000001.000100"); !reflect.DeepEqual(got, []domain.Message{full}) {
		t.Errorf("Replies = %+v, want [%+v]", got, full)
	}
}

func TestMessagesNewestFirstBeforeAndLimit(t *testing.T) {
	s := openStore(t)
	mustPutHistory(t, s, "C01",
		msg(t, "1700000002.000000", "b"),
		msg(t, "1700000004.000000", "d"),
		msg(t, "1700000001.000000", "a"),
		msg(t, "1700000003.000000", "c"),
	)

	wantTexts(t, messages(t, s, "C01", zeroTS, 10), "d", "c", "b", "a")
	wantTexts(t, messages(t, s, "C01", zeroTS, 2), "d", "c")
	wantTexts(t, messages(t, s, "C01", ts(t, "1700000003.000000"), 10), "b", "a")
	wantTexts(t, messages(t, s, "C01", ts(t, "1700000003.000000"), 1), "b")
	wantTexts(t, messages(t, s, "C01", ts(t, "1700000001.000000"), 10))
}

func TestMessagesAreScopedToTheConversation(t *testing.T) {
	s := openStore(t)
	mustPutHistory(t, s, "C01", msg(t, "1700000001.000000", "in C01"))
	mustPutHistory(t, s, "D02", msg(t, "1700000002.000000", "in D02"))

	wantTexts(t, messages(t, s, "C01", zeroTS, 10), "in C01")
}

func TestPutHistoryTwiceIsIdempotent(t *testing.T) {
	s := openStore(t)
	page := []domain.Message{msg(t, "1700000001.000000", "a"), msg(t, "1700000002.000000", "b")}
	mustPutHistory(t, s, "C01", page...)
	mustPutHistory(t, s, "C01", page...)

	wantTexts(t, messages(t, s, "C01", zeroTS, 10), "b", "a")
}

func TestPutHistoryRefreshesAMessage(t *testing.T) {
	s := openStore(t)
	mustPutHistory(t, s, "C01", msg(t, "1700000001.000000", "old"))
	edited := msg(t, "1700000001.000000", "new")
	edited.Edited = ts(t, "1700000009.000000")
	mustPutHistory(t, s, "C01", edited)

	got := messages(t, s, "C01", zeroTS, 10)
	if !reflect.DeepEqual(got, []domain.Message{edited}) {
		t.Errorf("Messages = %+v, want %+v", got, edited)
	}
}

func TestOlderVersionNeverOverwritesANewerEdit(t *testing.T) {
	s := openStore(t)
	edited := msg(t, "1700000001.000000", "edited")
	edited.Edited = ts(t, "1700000009.000000")
	mustPutHistory(t, s, "C01", edited)
	mustPutHistory(t, s, "C01", msg(t, "1700000001.000000", "original")) // a stale page

	wantTexts(t, messages(t, s, "C01", zeroTS, 10), "edited")
}

// A corrupt row is never served as if it were valid: reading it either
// fails, or the row does not qualify for the query at all (a corrupt
// thread_ts is neither top-level nor the reply of any real thread).
func TestCorruptRowsAreNeverTrusted(t *testing.T) {
	for name, stmt := range map[string]string{
		"ts":        `INSERT INTO messages (conv_id, ts) VALUES ('C01', 'garbage')`,
		"thread_ts": `INSERT INTO messages (conv_id, ts, thread_ts) VALUES ('C01', '1700000001.000000', 'x')`,
		"author":    `INSERT INTO messages (conv_id, ts, author) VALUES ('C01', '1700000001.000000', 'nobody')`,
		"edited_ts": `INSERT INTO messages (conv_id, ts, edited_ts) VALUES ('C01', '1700000001.000000', '1.2')`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "T123.db")
			s := openAt(t, path)
			rawExec(t, path, stmt)
			got, err := s.Messages(context.Background(), conv(t, "C01"), zeroTS, 10)
			if err == nil && len(got) != 0 {
				t.Errorf("Messages served a row with a corrupt %s: %+v", name, got)
			}
		})
	}
}

func TestBotAuthorsAndTheirNamesRoundTrip(t *testing.T) {
	s := openStore(t)
	bot := domain.Message{TS: ts(t, "1700000001.000100"), Author: author(t, "B01"), AuthorName: "deploybot", Text: "deploy finished"}
	mustPutHistory(t, s, "C01", bot)

	got := messages(t, s, "C01", zeroTS, 10)
	if len(got) != 1 || !reflect.DeepEqual(got[0], bot) {
		t.Fatalf("Messages = %+v, want [%+v]", got, bot)
	}
	if _, ok := got[0].Author.Bot(); !ok {
		t.Error("the author came back as something other than a bot")
	}
}

// An edit's result (chat.update) may come without the bot's name; the
// message keeps the one it was posted with.
func TestAVersionWithoutANameKeepsTheStoredOne(t *testing.T) {
	s := openStore(t)
	bot := domain.Message{TS: ts(t, "1700000001.000100"), Author: author(t, "B01"), AuthorName: "deploybot", Text: "deploying"}
	mustPutHistory(t, s, "C01", bot)
	edit := bot
	edit.AuthorName, edit.Text, edit.Edited = "", "deployed", ts(t, "1700000009.000000")
	mustPutHistory(t, s, "C01", edit)

	got := messages(t, s, "C01", zeroTS, 10)
	if len(got) != 1 || got[0].AuthorName != "deploybot" || got[0].Text != "deployed" {
		t.Fatalf("Messages = %+v, want the edit named deploybot", got)
	}
}

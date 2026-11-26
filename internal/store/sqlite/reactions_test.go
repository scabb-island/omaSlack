package sqlite_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

const reactedTS = "1700000001.000000"

func react(t *testing.T, c, u, emoji string, added bool) domain.Event {
	t.Helper()
	return domain.ReactionChanged{
		Conversation: conv(t, c), TS: ts(t, reactedTS), User: user(t, u), Emoji: emoji, Added: added,
	}
}

func add(t *testing.T, u, emoji string) domain.Event    { return react(t, "C01", u, emoji, true) }
func remove(t *testing.T, u, emoji string) domain.Event { return react(t, "C01", u, emoji, false) }

// reactions renders the reactions of the reacted message in C01 as
// "emoji:user,user" strings, which shows order and grouping at a glance.
func reactions(t *testing.T, s store.Store, c string) []string {
	t.Helper()
	got, err := s.Reactions(context.Background(), conv(t, c), ts(t, reactedTS))
	if err != nil {
		t.Fatalf("Reactions: %v", err)
	}
	out := []string{}
	for _, r := range got {
		line := r.Emoji + ":"
		for i, u := range r.Users {
			if i > 0 {
				line += ","
			}
			line += u.String()
		}
		out = append(out, line)
	}
	return out
}

func wantReactions(t *testing.T, s store.Store, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := reactions(t, s, "C01"); !reflect.DeepEqual(got, want) {
		t.Errorf("reactions = %q, want %q", got, want)
	}
}

func TestNoReactions(t *testing.T) {
	wantReactions(t, openStore(t))
}

func TestReactionsKeepTheOrderAdded(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		add(t, "U01", "+1"),
		add(t, "U02", "tada"),
		add(t, "U03", "+1"),
	)
	wantReactions(t, s, "+1:U01,U03", "tada:U02")
}

func TestAddingTheSameReactionTwiceIsIdempotent(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		add(t, "U01", "+1"),
		add(t, "U02", "+1"),
		add(t, "U01", "+1"),
	)
	wantReactions(t, s, "+1:U01,U02")
}

func TestRemovingAReactionRemovesOnlyThatUser(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		add(t, "U01", "+1"),
		add(t, "U02", "+1"),
		add(t, "U01", "tada"),
		remove(t, "U01", "+1"),
	)
	wantReactions(t, s, "+1:U02", "tada:U01")
}

func TestAnEmojiNobodyHasIsGone(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		add(t, "U01", "+1"),
		add(t, "U01", "tada"),
		remove(t, "U01", "+1"),
		remove(t, "U01", "+1"),   // removing twice is fine
		remove(t, "U09", "eyes"), // and so is removing what was never there
	)
	wantReactions(t, s, "tada:U01")
}

func TestReactingAgainMovesToTheEnd(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		add(t, "U01", "+1"),
		add(t, "U02", "+1"),
		remove(t, "U01", "+1"),
		add(t, "U01", "+1"),
	)
	wantReactions(t, s, "+1:U02,U01")
}

func TestReactionsAreScopedToTheMessage(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		react(t, "D02", "U01", "+1", true),
		domain.ReactionChanged{
			Conversation: conv(t, "C01"), TS: ts(t, "1700000099.000000"),
			User: user(t, "U01"), Emoji: "+1", Added: true,
		},
	)
	wantReactions(t, s)
}

func TestDeletingAMessageDropsItsReactions(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		created(t, "C01", msg(t, reactedTS, "hi")),
		add(t, "U01", "+1"),
		deleted(t, "C01", reactedTS),
	)
	wantReactions(t, s)
}

func TestAReactionAfterTheDeleteIsIgnored(t *testing.T) {
	s := openStore(t)
	mustApply(t, s,
		deleted(t, "C01", reactedTS),
		add(t, "U01", "+1"),
	)
	wantReactions(t, s)
}

func TestCorruptReactionIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "T123.db")
	s := openAt(t, path)
	rawExec(t, path, `INSERT INTO reactions (conv_id, ts, emoji, user_id)
		VALUES ('C01', '`+reactedTS+`', '+1', 'someone')`)
	if _, err := s.Reactions(context.Background(), conv(t, "C01"), ts(t, reactedTS)); err == nil {
		t.Error("Reactions accepted a corrupt user")
	}
}

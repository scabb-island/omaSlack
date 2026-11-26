package domain

import (
	"errors"
	"testing"
)

func TestAuthorIsAUserOrABot(t *testing.T) {
	user, _ := ParseUserID("U0123ABCD")
	bot, _ := ParseBotID("B0123ABCD")

	a := UserAuthor(user)
	if got, ok := a.User(); !ok || got != user {
		t.Errorf("UserAuthor.User() = (%v, %v), want (%v, true)", got, ok, user)
	}
	if _, ok := a.Bot(); ok {
		t.Error("a user author also claims to be a bot")
	}

	b := BotAuthor(bot)
	if got, ok := b.Bot(); !ok || got != bot {
		t.Errorf("BotAuthor.Bot() = (%v, %v), want (%v, true)", got, ok, bot)
	}
	if _, ok := b.User(); ok {
		t.Error("a bot author also claims to be a user")
	}
}

func TestParseAuthorRoundTrips(t *testing.T) {
	for _, s := range []string{"U0123ABCD", "W0123ABCD", "B0123ABCD"} {
		a, err := ParseAuthor(s)
		if err != nil || a.String() != s {
			t.Errorf("ParseAuthor(%q) = (%q, %v), want (%q, nil)", s, a.String(), err, s)
		}
	}
	a, _ := ParseAuthor("B0123ABCD")
	if _, ok := a.Bot(); !ok {
		t.Error("a B… ID did not parse as a bot")
	}
}

func TestParseAuthorRejectsOtherIDs(t *testing.T) {
	for _, s := range []string{"", "C0123ABCD", "T0123ABCD", "b0123ABCD"} {
		a, err := ParseAuthor(s)
		if !errors.Is(err, ErrInvalidID) {
			t.Errorf("ParseAuthor(%q) err = %v, want ErrInvalidID", s, err)
		}
		if !a.IsZero() {
			t.Errorf("ParseAuthor(%q) leaked %q", s, a.String())
		}
	}
}

func TestZeroAuthor(t *testing.T) {
	var a Author
	if !a.IsZero() {
		t.Error("the zero Author is not zero")
	}
	if _, ok := a.User(); ok {
		t.Error("the zero Author claims to be a user")
	}
	if _, ok := a.Bot(); ok {
		t.Error("the zero Author claims to be a bot")
	}
	user, _ := ParseUserID("U0123ABCD")
	if UserAuthor(user).IsZero() {
		t.Error("a user author reports zero")
	}
}

func TestParseBotID(t *testing.T) {
	if _, err := ParseBotID("B0123ABCD"); err != nil {
		t.Errorf("ParseBotID(B0123ABCD) = %v", err)
	}
	if id, err := ParseBotID("U0123ABCD"); !errors.Is(err, ErrInvalidID) || !id.IsZero() {
		t.Errorf("a user ID parsed as a bot ID: (%q, %v)", id, err)
	}
}

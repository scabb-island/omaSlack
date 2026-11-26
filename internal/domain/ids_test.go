package domain

import (
	"errors"
	"testing"
)

func TestParseIDsAcceptSlackShapes(t *testing.T) {
	cases := []struct {
		kind  string
		in    string
		parse func(string) (string, error)
	}{
		{"shortest team", "TZ9", func(s string) (string, error) { id, err := ParseTeamID(s); return id.String(), err }},
		{"team", "T0123ABCD", func(s string) (string, error) { id, err := ParseTeamID(s); return id.String(), err }},
		{"enterprise team", "E0123ABCD", func(s string) (string, error) { id, err := ParseTeamID(s); return id.String(), err }},
		{"user", "U0123ABCD", func(s string) (string, error) { id, err := ParseUserID(s); return id.String(), err }},
		{"enterprise user", "W0123ABCD", func(s string) (string, error) { id, err := ParseUserID(s); return id.String(), err }},
		{"channel", "C0123ABCD", func(s string) (string, error) { id, err := ParseConversationID(s); return id.String(), err }},
		{"private channel", "G0123ABCD", func(s string) (string, error) { id, err := ParseConversationID(s); return id.String(), err }},
		{"direct message", "D0123ABCD", func(s string) (string, error) { id, err := ParseConversationID(s); return id.String(), err }},
	}
	for _, c := range cases {
		got, err := c.parse(c.in)
		if err != nil || got != c.in {
			t.Errorf("%s %q: got (%q, %v), want (%q, nil)", c.kind, c.in, got, err, c.in)
		}
	}
}

func TestParseIDsRejectOtherShapes(t *testing.T) {
	bad := []string{"", "T", "T0", "t0123ABCD", "X0123ABCD", "T0123 ABCD", "T0123abcd", " T0123ABCD"}
	for _, s := range bad {
		if _, err := ParseTeamID(s); !errors.Is(err, ErrInvalidID) {
			t.Errorf("ParseTeamID(%q) err = %v, want ErrInvalidID", s, err)
		}
	}
	if _, err := ParseUserID("C0123ABCD"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("a channel ID parsed as a user ID")
	}
	if _, err := ParseConversationID("U0123ABCD"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("a user ID parsed as a conversation ID")
	}
}

func TestFailedParsesReturnZeroIDs(t *testing.T) {
	team, _ := ParseTeamID("bogus")
	user, _ := ParseUserID("bogus")
	conv, _ := ParseConversationID("bogus")
	if !team.IsZero() || !user.IsZero() || !conv.IsZero() {
		t.Errorf("a failed parse leaked a value: %q %q %q", team, user, conv)
	}
}

func TestZeroIDs(t *testing.T) {
	var team TeamID
	if !team.IsZero() {
		t.Error("the zero TeamID is not zero")
	}
	id, _ := ParseTeamID("T0123ABCD")
	if id.IsZero() {
		t.Error("a parsed TeamID reports zero")
	}
}

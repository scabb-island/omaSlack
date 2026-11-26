// Package domain holds the Slack domain types every other package shares:
// identifiers, timestamps, provider capabilities and the normalized events
// the sync engine publishes. Values are parsed once, where outside data
// enters (provider responses, RPC requests); inside the daemon they are
// trusted. The glossary lives in openspec/project.md, "Domain Context".
package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidID reports a string that is not a Slack identifier of the
// expected kind.
var ErrInvalidID = errors.New("invalid Slack ID")

// TeamID identifies a workspace: T… for a workspace, E… for an Enterprise
// Grid organization. The zero value means "no team".
type TeamID struct{ s string }

// UserID identifies a person: U… or, in Enterprise Grid, W….
type UserID struct{ s string }

// ConversationID identifies a channel (C…), a private channel or group DM
// (G…) or a direct message (D…).
type ConversationID struct{ s string }

// ParseTeamID validates s as a team ID.
func ParseTeamID(s string) (TeamID, error) {
	return parseID(s, "team", "TE", func(v string) TeamID { return TeamID{v} })
}

// ParseUserID validates s as a user ID.
func ParseUserID(s string) (UserID, error) {
	return parseID(s, "user", "UW", func(v string) UserID { return UserID{v} })
}

// ParseConversationID validates s as a conversation ID.
func ParseConversationID(s string) (ConversationID, error) {
	return parseID(s, "conversation", "CGD", func(v string) ConversationID { return ConversationID{v} })
}

// parseID wraps s only when it is valid, so a failed parse never leaks a
// non-zero ID.
func parseID[T any](s, kind, prefixes string, wrap func(string) T) (T, error) {
	if err := checkID(s, kind, prefixes); err != nil {
		var zero T
		return zero, err
	}
	return wrap(s), nil
}

func (id TeamID) String() string         { return id.s }
func (id UserID) String() string         { return id.s }
func (id ConversationID) String() string { return id.s }

func (id TeamID) IsZero() bool         { return id.s == "" }
func (id UserID) IsZero() bool         { return id.s == "" }
func (id ConversationID) IsZero() bool { return id.s == "" }

// checkID is the single definition of a well-formed Slack ID: one of the
// allowed prefix letters followed by at least two upper-case letters or
// digits. The prefix is itself an upper-case letter, so the whole ID is
// checked in one pass.
func checkID(s, kind, prefixes string) error {
	if len(s) < 3 || !strings.ContainsRune(prefixes, rune(s[0])) {
		return fmt.Errorf("%w: %q is not a %s ID", ErrInvalidID, s, kind)
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return fmt.Errorf("%w: %q is not a %s ID", ErrInvalidID, s, kind)
		}
	}
	return nil
}

package domain

import "testing"

// The topic of each domain event is the name clients subscribe to with
// events.subscribe (spec daemon-api), so it is part of the protocol.
func TestEventTopicsMatchTheProtocol(t *testing.T) {
	cases := map[string]Event{
		"message.created":  MessageCreated{},
		"message.updated":  MessageUpdated{},
		"message.deleted":  MessageDeleted{},
		"reaction.changed": ReactionChanged{},
		"read.changed":     ReadChanged{},
		"presence.changed": PresenceChanged{},
		"typing":           UserTyping{},
		"connection.state": ConnectionChanged{},
	}
	for want, ev := range cases {
		if got := ev.Topic(); got != want {
			t.Errorf("%T.Topic() = %q, want %q", ev, got, want)
		}
	}
}

func TestEventsCarryTheirTeam(t *testing.T) {
	team, _ := ParseTeamID("T0123ABCD")
	events := []Event{
		MessageCreated{Team: team}, MessageUpdated{Team: team}, MessageDeleted{Team: team},
		ReactionChanged{Team: team}, ReadChanged{Team: team}, PresenceChanged{Team: team},
		UserTyping{Team: team}, ConnectionChanged{Team: team},
	}
	for _, ev := range events {
		if ev.TeamID() != team {
			t.Errorf("%T.TeamID() = %v, want %v", ev, ev.TeamID(), team)
		}
	}
}

func TestMessageIsReply(t *testing.T) {
	root, _ := ParseTS("1700000000.000001")
	reply, _ := ParseTS("1700000005.000001")
	cases := []struct {
		name string
		msg  Message
		want bool
	}{
		{"top-level message", Message{TS: root}, false},
		{"thread root", Message{TS: root, ThreadTS: root}, false},
		{"thread reply", Message{TS: reply, ThreadTS: root}, true},
	}
	for _, c := range cases {
		if got := c.msg.IsReply(); got != c.want {
			t.Errorf("%s: IsReply() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestConnectionStateNames(t *testing.T) {
	want := map[ConnectionState]string{
		Connecting: "connecting", Connected: "connected", Disconnected: "disconnected",
		RateLimited: "rate_limited", AuthExpired: "auth_expired",
	}
	for s, name := range want {
		if s.String() != name {
			t.Errorf("state %d is %q, want %q", s, s.String(), name)
		}
	}
}

package slackwire

import (
	"encoding/json"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// mapJSON maps one event the way a provider does: Kind picks the mapper,
// then the whole event is decoded for it.
func mapJSON(t *testing.T, team domain.TeamID, js string) (domain.Event, bool, error) {
	t.Helper()
	var head Head
	if err := json.Unmarshal([]byte(js), &head); err != nil {
		t.Fatal(err)
	}
	mapEvent, ok := MapperFor(head.Kind())
	if !ok {
		return nil, false, nil
	}
	var e Event
	if err := json.Unmarshal([]byte(js), &e); err != nil {
		t.Fatal(err)
	}
	ev, err := mapEvent(team, e)
	return ev, true, err
}

func TestEventsMapToDomainEvents(t *testing.T) {
	team := must(t, domain.ParseTeamID, "T0001")
	conv := func(s string) domain.ConversationID { return must(t, domain.ParseConversationID, s) }
	ts := func(s string) domain.TS { return must(t, domain.ParseTS, s) }
	msg := domain.Message{TS: ts("1700000600.000600"), Author: must(t, domain.ParseAuthor, "U0002"), Text: "hi"}
	user := must(t, domain.ParseUserID, "U0002")
	created := domain.MessageCreated{Team: team, Conversation: conv("C0001"), Message: msg}
	const newMessage = `"channel":"C0001","user":"U0002","text":"hi","ts":"1700000600.000600"}`

	for name, tc := range map[string]struct {
		json string
		want domain.Event
	}{
		"message":          {`{"type":"message",` + newMessage, created},
		"thread_broadcast": {`{"type":"message","subtype":"thread_broadcast",` + newMessage, created},
		"file_share":       {`{"type":"message","subtype":"file_share",` + newMessage, created},
		"me_message":       {`{"type":"message","subtype":"me_message",` + newMessage, created},
		"bot_message": {`{"type":"message","subtype":"bot_message","channel":"C0001","bot_id":"B0001","ts":"1700000600.000600"}`,
			domain.MessageCreated{Team: team, Conversation: conv("C0001"),
				Message: domain.Message{TS: ts("1700000600.000600"), Author: must(t, domain.ParseAuthor, "B0001")}}},
		"message_changed": {`{"type":"message","subtype":"message_changed","channel":"C0001","ts":"1700000700.000700",` +
			`"message":{"user":"U0002","text":"hi","ts":"1700000600.000600"}}`,
			domain.MessageUpdated{Team: team, Conversation: conv("C0001"), Message: msg}},
		"message_deleted": {`{"type":"message","subtype":"message_deleted","channel":"G0001","deleted_ts":"1700000600.000600"}`,
			domain.MessageDeleted{Team: team, Conversation: conv("G0001"), TS: ts("1700000600.000600")}},
		"reaction_added": {`{"type":"reaction_added","user":"U0002","reaction":"tada","item":{"type":"message","channel":"D0001","ts":"1700000600.000600"}}`,
			domain.ReactionChanged{Team: team, Conversation: conv("D0001"), TS: ts("1700000600.000600"), User: user, Emoji: "tada", Added: true}},
		"reaction_removed": {`{"type":"reaction_removed","user":"U0002","reaction":"tada","item":{"type":"message","channel":"D0001","ts":"1700000600.000600"}}`,
			domain.ReactionChanged{Team: team, Conversation: conv("D0001"), TS: ts("1700000600.000600"), User: user, Emoji: "tada"}},
		"reaction to a file": {`{"type":"reaction_added","user":"U0002","reaction":"tada","item":{"type":"file","file":"F0001"}}`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := mapJSON(t, team, tc.json)
			if !ok || err != nil {
				t.Fatalf("mapped = %v, err = %v", ok, err)
			}
			if got != tc.want {
				t.Fatalf("got\n%#v\nwant\n%#v", got, tc.want)
			}
		})
	}
}

func TestKindsWithoutAMapper(t *testing.T) {
	for _, js := range []string{
		`{"type":"message","subtype":"channel_join"}`,
		`{"type":"channel_created"}`,
		`{"type":"message_changed"}`, // a subtype is not a type
	} {
		if _, ok, _ := mapJSON(t, must(t, domain.ParseTeamID, "T0001"), js); ok {
			t.Errorf("%s has a mapper", js)
		}
	}
}

func TestMalformedEventsAreRejected(t *testing.T) {
	for name, js := range map[string]string{
		"message bad channel":        `{"type":"message","channel":"U0001","user":"U0002","ts":"1700000600.000600"}`,
		"message bad ts":             `{"type":"message","channel":"C0001","user":"U0002","ts":"now"}`,
		"changed bad inner message":  `{"type":"message","subtype":"message_changed","channel":"C0001","message":{"ts":"1700000600.000600"}}`,
		"deleted bad channel":        `{"type":"message","subtype":"message_deleted","channel":"X","deleted_ts":"1700000600.000600"}`,
		"deleted bad deleted_ts":     `{"type":"message","subtype":"message_deleted","channel":"C0001","deleted_ts":""}`,
		"reaction bad channel":       `{"type":"reaction_added","user":"U0002","reaction":"x","item":{"type":"message","channel":"","ts":"1700000600.000600"}}`,
		"reaction bad ts":            `{"type":"reaction_added","user":"U0002","reaction":"x","item":{"type":"message","channel":"C0001","ts":"1"}}`,
		"reaction bad user":          `{"type":"reaction_removed","user":"","reaction":"x","item":{"type":"message","channel":"C0001","ts":"1700000600.000600"}}`,
		"reaction without its emoji": `{"type":"reaction_added","user":"U0002","reaction":"","item":{"type":"message","channel":"C0001","ts":"1700000600.000600"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, _, err := mapJSON(t, must(t, domain.ParseTeamID, "T0001"), js)
			if err == nil || got != nil {
				t.Fatalf("got %v, %v; want nil and an error", got, err)
			}
		})
	}
}

package internalapp

import (
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func TestRecordedEventsMapToDomainEvents(t *testing.T) {
	team := mustTeam(t, "T0001")
	cases := map[string]domain.Event{
		"message.json": domain.MessageCreated{
			Team:         team,
			Conversation: mustConv(t, "C0001"),
			Message:      domain.Message{TS: mustTS(t, "1700000600.000600"), Author: mustAuthor(t, "U0002"), Text: "hello <@U0001>"},
		},
		"message_thread_broadcast.json": domain.MessageCreated{
			Team:         team,
			Conversation: mustConv(t, "C0001"),
			Message: domain.Message{
				TS:       mustTS(t, "1700000650.000650"),
				ThreadTS: mustTS(t, "1700000100.000100"),
				Author:   mustAuthor(t, "U0002"),
				Text:     "also sent to the channel",
			},
		},
		"message_changed.json": domain.MessageUpdated{
			Team:         team,
			Conversation: mustConv(t, "C0001"),
			Message: domain.Message{
				TS:     mustTS(t, "1700000600.000600"),
				Author: mustAuthor(t, "U0002"),
				Text:   "hello, edited",
				Edited: mustTS(t, "1700000700.000000"),
			},
		},
		"message_deleted.json": domain.MessageDeleted{
			Team:         team,
			Conversation: mustConv(t, "G0001"),
			TS:           mustTS(t, "1700000600.000600"),
		},
		"reaction_added.json": domain.ReactionChanged{
			Team:         team,
			Conversation: mustConv(t, "D0001"),
			TS:           mustTS(t, "1700000600.000600"),
			User:         mustUser(t, "U0002"),
			Emoji:        "thumbsup",
			Added:        true,
		},
		"reaction_removed.json": domain.ReactionChanged{
			Team:         team,
			Conversation: mustConv(t, "D0001"),
			TS:           mustTS(t, "1700000600.000600"),
			User:         mustUser(t, "U0002"),
			Emoji:        "thumbsup",
		},
		// Not a message the domain tracks: ignored without an error.
		"message_channel_join.json":   nil,
		"reaction_added_to_file.json": nil,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := mapCallback(team, readFixture(t, "events/"+name))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("mapCallback() =\n%#v\nwant\n%#v", got, want)
			}
		})
	}
}

func TestBotMessagesMapWithABotAuthor(t *testing.T) {
	payload := `{"type":"event_callback","team_id":"T0001","event":{"type":"message","subtype":"bot_message","channel":"C0001","bot_id":"B0001","text":"deploy finished","ts":"1700000600.000600"}}`
	got, err := mapCallback(mustTeam(t, "T0001"), []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	want := domain.MessageCreated{
		Team:         mustTeam(t, "T0001"),
		Conversation: mustConv(t, "C0001"),
		Message:      domain.Message{TS: mustTS(t, "1700000600.000600"), Author: mustAuthor(t, "B0001"), Text: "deploy finished"},
	}
	if got != want {
		t.Errorf("mapCallback() = %#v, want %#v", got, want)
	}
}

func TestEventsTheProviderDoesNotConsumeAreIgnored(t *testing.T) {
	for name, payload := range map[string]string{
		"other event type": `{"type":"event_callback","event":{"type":"channel_created","channel":{"id":"C0002"}}}`,
		"no event":         `{"type":"app_rate_limited","team_id":"T0001","minute_rate_limited":1700000000}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := mapCallback(mustTeam(t, "T0001"), []byte(payload))
			if got != nil || err != nil {
				t.Fatalf("mapCallback() = %v, %v; want nil, nil", got, err)
			}
		})
	}
}

func TestMalformedEventsAreRejected(t *testing.T) {
	for name, event := range map[string]string{
		"not json":                   `{`,
		"message channel as object":  `{"type":"message","channel":{"id":"C0001"},"user":"U0002","ts":"1700000600.000600"}`,
		"message bad channel":        `{"type":"message","channel":"U0001","user":"U0002","text":"x","ts":"1700000600.000600"}`,
		"message bad ts":             `{"type":"message","channel":"C0001","user":"U0002","text":"x","ts":"now"}`,
		"message without an author":  `{"type":"message","subtype":"file_share","channel":"C0001","ts":"1700000600.000600"}`,
		"me_message bad thread":      `{"type":"message","subtype":"me_message","channel":"C0001","user":"U0002","ts":"1700000600.000600","thread_ts":"x"}`,
		"changed bad channel":        `{"type":"message","subtype":"message_changed","channel":"","message":{"user":"U0002","ts":"1700000600.000600"}}`,
		"changed bad inner message":  `{"type":"message","subtype":"message_changed","channel":"C0001","message":{"ts":"1700000600.000600"}}`,
		"deleted bad channel":        `{"type":"message","subtype":"message_deleted","channel":"X","deleted_ts":"1700000600.000600"}`,
		"deleted bad deleted_ts":     `{"type":"message","subtype":"message_deleted","channel":"C0001","deleted_ts":""}`,
		"reaction bad channel":       `{"type":"reaction_added","user":"U0002","reaction":"x","item":{"type":"message","channel":"","ts":"1700000600.000600"}}`,
		"reaction bad ts":            `{"type":"reaction_added","user":"U0002","reaction":"x","item":{"type":"message","channel":"C0001","ts":"1"}}`,
		"reaction bad user":          `{"type":"reaction_removed","user":"","reaction":"x","item":{"type":"message","channel":"C0001","ts":"1700000600.000600"}}`,
		"reaction without its emoji": `{"type":"reaction_added","user":"U0002","reaction":"","item":{"type":"message","channel":"C0001","ts":"1700000600.000600"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			payload := `{"type":"event_callback","team_id":"T0001","event":` + event + `}`
			got, err := mapCallback(mustTeam(t, "T0001"), []byte(payload))
			if err == nil || got != nil {
				t.Fatalf("mapCallback() = %v, %v; want nil and an error", got, err)
			}
		})
	}
}

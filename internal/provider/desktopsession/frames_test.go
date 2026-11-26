package desktopsession

import (
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func TestRecordedFramesMapToDomainEvents(t *testing.T) {
	team := parse(t, domain.ParseTeamID, "T0001")
	conv := func(s string) domain.ConversationID { return parse(t, domain.ParseConversationID, s) }
	ts := func(s string) domain.TS { return parse(t, domain.ParseTS, s) }
	user := func(s string) domain.UserID { return parse(t, domain.ParseUserID, s) }
	author := func(s string) domain.Author { return parse(t, domain.ParseAuthor, s) }

	cases := map[string][]domain.Event{
		"hello.json": {domain.ConnectionChanged{Team: team, State: domain.Connected}},
		"message.json": {domain.MessageCreated{Team: team, Conversation: conv("C0001"),
			Message: domain.Message{TS: ts("1700000600.000600"), Author: author("U0002"), Text: "hello <@U0001>"}}},
		"message_bot.json": {domain.MessageCreated{Team: team, Conversation: conv("C0001"),
			Message: domain.Message{TS: ts("1700000610.000610"), Author: author("B0001"), Text: "deploy finished"}}},
		"message_thread_reply.json": {domain.MessageCreated{Team: team, Conversation: conv("C0001"),
			Message: domain.Message{TS: ts("1700000620.000620"), ThreadTS: ts("1700000100.000100"), Author: author("U0002"), Text: "a reply"}}},
		"message_file_share.json": {domain.MessageCreated{Team: team, Conversation: conv("C0001"),
			Message: domain.Message{TS: ts("1700000660.000660"), Author: author("U0002"), Text: "the screenshot", Files: []domain.File{{
				ID: parse(t, domain.ParseFileID, "F0001"), Name: "screenshot.png", Title: "screenshot.png", MIMEType: "image/png", Size: 48213,
				DownloadURL: "https://files.slack.com/files-pri/T0001-F0001/download/screenshot.png",
			}}}}},
		"message_changed.json": {domain.MessageUpdated{Team: team, Conversation: conv("C0001"),
			Message: domain.Message{TS: ts("1700000600.000600"), Author: author("U0002"), Text: "hello, edited", Edited: ts("1700000700.000000")}}},
		"message_deleted.json": {domain.MessageDeleted{Team: team, Conversation: conv("G0001"), TS: ts("1700000600.000600")}},
		"reaction_added.json": {domain.ReactionChanged{Team: team, Conversation: conv("D0001"), TS: ts("1700000600.000600"),
			User: user("U0002"), Emoji: "thumbsup", Added: true}},
		"reaction_removed.json": {domain.ReactionChanged{Team: team, Conversation: conv("D0001"), TS: ts("1700000600.000600"),
			User: user("U0002"), Emoji: "thumbsup"}},
		"channel_marked.json":  {domain.ReadChanged{Team: team, Conversation: conv("C0001"), LastRead: ts("1700000600.000600")}},
		"im_marked.json":       {domain.ReadChanged{Team: team, Conversation: conv("D0001"), LastRead: ts("1700000601.000601")}},
		"group_marked.json":    {domain.ReadChanged{Team: team, Conversation: conv("G0001"), LastRead: ts("1700000602.000602")}},
		"mpim_marked.json":     {domain.ReadChanged{Team: team, Conversation: conv("G0002"), LastRead: ts("1700000603.000603")}},
		"presence_change.json": {domain.PresenceChanged{Team: team, User: user("U0002"), Presence: domain.Active}},
		"presence_change_batch.json": {
			domain.PresenceChanged{Team: team, User: user("U0002"), Presence: domain.Away},
			domain.PresenceChanged{Team: team, User: user("U0003"), Presence: domain.Away},
		},
		"user_typing.json": {domain.UserTyping{Team: team, Conversation: conv("C0001"), User: user("U0002")}},
		// Nothing the domain tracks: ignored without an error.
		"message_ephemeral.json":    nil,
		"message_channel_join.json": nil,
		"thread_marked.json":        nil,
		"reconnect_url.json":        nil,
		"goodbye.json":              nil,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := mapFrame(team, fixture(t, "frames/"+name))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("mapFrame() =\n%#v\nwant\n%#v", got, want)
			}
		})
	}
}

func TestMalformedFramesAreRejected(t *testing.T) {
	team := parse(t, domain.ParseTeamID, "T0001")
	for name, frame := range map[string]string{
		"not json":                  `{`,
		"message bad ts":            `{"type":"message","channel":"C0001","user":"U0002","ts":"now"}`,
		"reaction without its user": `{"type":"reaction_added","reaction":"x","item":{"type":"message","channel":"C0001","ts":"1700000600.000600"}}`,
		"marked bad channel":        `{"type":"channel_marked","channel":"U0001","ts":"1700000600.000600"}`,
		"marked without a ts":       `{"type":"im_marked","channel":"D0001"}`,
		"presence bad user":         `{"type":"presence_change","presence":"active","user":"C0001"}`,
		"presence bad batch user":   `{"type":"presence_change","presence":"away","users":["U0002","nope"]}`,
		"presence with no user":     `{"type":"presence_change","presence":"away"}`,
		"presence unknown state":    `{"type":"presence_change","presence":"busy","user":"U0002"}`,
		"typing bad channel":        `{"type":"user_typing","channel":"","user":"U0002"}`,
		"typing bad user":           `{"type":"user_typing","channel":"C0001","user":"B0001"}`,
		"users as a string":         `{"type":"presence_change","presence":"away","users":"U0002"}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := mapFrame(team, []byte(frame))
			if err == nil || got != nil {
				t.Fatalf("mapFrame() = %v, %v; want nil and an error", got, err)
			}
		})
	}
}

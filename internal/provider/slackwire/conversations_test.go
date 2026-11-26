package slackwire

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func decodeConversation(t *testing.T, js string) Conversation {
	t.Helper()
	var w Conversation
	if err := json.Unmarshal([]byte(js), &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestConversationsParseWithTheirKind(t *testing.T) {
	conv := func(s string) domain.ConversationID { return must(t, domain.ParseConversationID, s) }
	for name, tc := range map[string]struct {
		json string
		want domain.Conversation
	}{
		"public channel": {`{"id":"C0001","name":"general","is_channel":true,"is_private":false}`,
			domain.Conversation{ID: conv("C0001"), Kind: domain.Channel, Name: "general"}},
		"private channel": {`{"id":"C0002","name":"secret","is_channel":true,"is_private":true}`,
			domain.Conversation{ID: conv("C0002"), Kind: domain.PrivateChannel, Name: "secret"}},
		"legacy private group": {`{"id":"G0001","name":"old","is_group":true,"is_private":true}`,
			domain.Conversation{ID: conv("G0001"), Kind: domain.PrivateChannel, Name: "old"}},
		// An mpim is private too: is_mpim decides first.
		"group DM": {`{"id":"G0002","name":"mpdm-a--b--c-1","is_group":true,"is_private":true,"is_mpim":true}`,
			domain.Conversation{ID: conv("G0002"), Kind: domain.GroupMessage, Name: "mpdm-a--b--c-1"}},
		"DM": {`{"id":"D0001","is_im":true,"user":"U0002"}`,
			domain.Conversation{ID: conv("D0001"), Kind: domain.DirectMessage}},
		"with a read marker": {`{"id":"C0001","name":"general","is_channel":true,"last_read":"1700000100.000100"}`,
			domain.Conversation{ID: conv("C0001"), Kind: domain.Channel, Name: "general", LastRead: must(t, domain.ParseTS, "1700000100.000100")}},
		// Slack's "never read" marker is the zero timestamp.
		"never read": {`{"id":"D0001","is_im":true,"last_read":"0000000000.000000"}`,
			domain.Conversation{ID: conv("D0001"), Kind: domain.DirectMessage}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeConversation(t, tc.json).Parse()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("Parse() =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestConversationsWithABadFieldAreRejected(t *testing.T) {
	for name, tc := range map[string]struct {
		json string
		err  error
	}{
		"bad id":        {`{"id":"U0001","is_im":true}`, domain.ErrInvalidID},
		"bad last_read": {`{"id":"C0001","last_read":"yesterday"}`, domain.ErrInvalidTS},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeConversation(t, tc.json).Parse()
			if !errors.Is(err, tc.err) || got != (domain.Conversation{}) {
				t.Fatalf("Parse() = %+v, %v; want nothing and %v", got, err, tc.err)
			}
		})
	}
}

func TestUserConversationsFollowsEveryPageAndDropsWhatDoesNotParse(t *testing.T) {
	api, s, logs := fakeSlack(t, map[string]map[string]string{"users.conversations": {
		"": `{"ok":true,"channels":[{"id":"C0001","name":"general","is_channel":true},{"id":"X0001","name":"odd"}],
			"response_metadata":{"next_cursor":"page2"}}`,
		"page2": `{"ok":true,"channels":[{"id":"D0001","is_im":true,"user":"U0002"}],"response_metadata":{"next_cursor":""}}`,
	}})

	got, err := api.UserConversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.Conversation{
		{ID: must(t, domain.ParseConversationID, "C0001"), Kind: domain.Channel, Name: "general"},
		{ID: must(t, domain.ParseConversationID, "D0001"), Kind: domain.DirectMessage},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UserConversations() =\n%+v\nwant\n%+v", got, want)
	}
	if !strings.Contains(logs.String(), "X0001") {
		t.Errorf("logs = %q, want the dropped conversation named", logs)
	}
	first := s.form(0)
	for key, value := range map[string]string{
		"types": "public_channel,private_channel,mpim,im", "exclude_archived": "true", "limit": "200",
	} {
		if first.Get(key) != value {
			t.Errorf("form %s = %q, want %q", key, first.Get(key), value)
		}
	}
	if first.Has("cursor") || s.form(1).Get("cursor") != "page2" {
		t.Errorf("cursors = %q then %q, want none then page2", first.Get("cursor"), s.form(1).Get("cursor"))
	}
}

func TestUserConversationsFailsWhenAPageFails(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"users.conversations": {
		"":      `{"ok":true,"channels":[{"id":"C0001","is_channel":true}],"response_metadata":{"next_cursor":"page2"}}`,
		"page2": `{"ok":false,"error":"internal_error"}`,
	}})

	got, err := api.UserConversations(context.Background())
	if err == nil || got != nil {
		t.Fatalf("UserConversations() = %v, %v; want nil and an error", got, err)
	}
}

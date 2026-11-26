package slackwire

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

func TestSearchMessagesReturnsHitsNewestFirstAndDropsWhatDoesNotParse(t *testing.T) {
	api, s, logs := fakeSlack(t, map[string]map[string]string{"search.messages": {"": `{"ok":true,"query":"deploy",
		"messages":{"total":3,"matches":[
			{"type":"message","user":"U0002","text":"deploy done","ts":"1700000200.000200","channel":{"id":"C0001","name":"ops"}},
			{"type":"message","bot_id":"B0001","text":"deploy started","ts":"1700000100.000100","channel":{"id":"D0001"}},
			{"type":"message","user":"U0002","text":"no channel","ts":"1700000050.000050","channel":{}}]}}`}})

	got, err := api.SearchMessages(context.Background(), "deploy in:#ops", 20)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.SearchHit{
		{Conversation: must(t, domain.ParseConversationID, "C0001"), Message: domain.Message{
			TS: must(t, domain.ParseTS, "1700000200.000200"), Author: must(t, domain.ParseAuthor, "U0002"), Text: "deploy done"}},
		{Conversation: must(t, domain.ParseConversationID, "D0001"), Message: domain.Message{
			TS: must(t, domain.ParseTS, "1700000100.000100"), Author: must(t, domain.ParseAuthor, "B0001"), Text: "deploy started"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchMessages() =\n%+v\nwant\n%+v", got, want)
	}
	if !strings.Contains(logs.String(), "1700000050.000050") {
		t.Errorf("logs = %q, want the dropped hit", logs.String())
	}
	wantForm := url.Values{"query": {"deploy in:#ops"}, "count": {"20"}, "sort": {"timestamp"}, "sort_dir": {"desc"}, "token": {"secret"}}
	if f := s.form(0); !reflect.DeepEqual(f, wantForm) {
		t.Errorf("form = %v, want %v", f, wantForm)
	}
}

func TestSearchMessagesAsksForOnePageAtMost(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"search.messages": {"": `{"ok":true,"messages":{"matches":[]}}`}})

	for _, limit := range []int{0, 500} {
		got, err := api.SearchMessages(context.Background(), "x", limit)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("SearchMessages(limit %d) = %#v, %v; want an empty list", limit, got, err)
		}
	}
	if s.form(0).Get("count") != "100" || s.form(1).Get("count") != "100" {
		t.Errorf("counts = %q, %q; want 100 (Slack's page maximum) for both", s.form(0).Get("count"), s.form(1).Get("count"))
	}
}

func TestSetPresenceSendsAutoOrAway(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"users.setPresence": {"": `{"ok":true}`}})

	for i, away := range []bool{true, false} {
		if err := api.SetPresence(context.Background(), away); err != nil {
			t.Fatal(err)
		}
		want := map[bool]string{true: "away", false: "auto"}[away]
		if got := s.form(i).Get("presence"); got != want {
			t.Errorf("SetPresence(%v) sent presence=%q, want %q", away, got, want)
		}
	}
}

func TestSearchAndPresenceReportSlackErrors(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{
		"search.messages":   {"": `{"ok":false,"error":"invalid_auth"}`},
		"users.setPresence": {"": `{"ok":false,"error":"invalid_presence"}`},
	})

	if _, err := api.SearchMessages(context.Background(), "x", 1); !errors.Is(err, provider.ErrAuthExpired) {
		t.Errorf("SearchMessages() err = %v, want ErrAuthExpired", err)
	}
	if err := api.SetPresence(context.Background(), true); !HasCode(err, "invalid_presence") {
		t.Errorf("SetPresence() err = %v, want invalid_presence", err)
	}
}

func TestSearchAndPresenceNeedTheirCapability(t *testing.T) {
	api, s, _ := fakeSlack(t, nil)
	api.caps = domain.NewCapabilities(domain.Realtime)

	if _, err := api.SearchMessages(context.Background(), "x", 1); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("SearchMessages() err = %v, want ErrUnsupported", err)
	}
	if err := api.SetPresence(context.Background(), true); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("SetPresence() err = %v, want ErrUnsupported", err)
	}
	if len(s.forms) != 0 {
		t.Errorf("%d requests, want none", len(s.forms))
	}
}

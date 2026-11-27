package slackwire

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

var emojiPages = map[string]map[string]string{"emoji.list": {
	"": `{"ok":true,"emoji":{"partyparrot":"https://emoji.slack-edge.com/T1/partyparrot/abc.gif","shipit":"alias:squirrel"},"cache_ts":"1700000000.000000"}`,
}}

func TestEmojiListsTheCustomEmojiOncePerTTL(t *testing.T) {
	api, s, _ := fakeSlack(t, emojiPages)
	now := time.Unix(1700000000, 0)
	api.now = func() time.Time { return now }
	want := map[string]string{"partyparrot": "https://emoji.slack-edge.com/T1/partyparrot/abc.gif", "shipit": "alias:squirrel"}
	list := func() {
		t.Helper()
		got, err := api.Emoji(context.Background())
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("Emoji() = %v, %v; want %v", got, err, want)
		}
		got["partyparrot"] = "changed by the caller"
	}

	list()
	now = now.Add(emojiTTL - time.Second)
	list()
	if len(s.forms) != 1 {
		t.Fatalf("%d requests within the TTL, want 1", len(s.forms))
	}
	now = now.Add(time.Second)
	list()
	if len(s.forms) != 2 {
		t.Errorf("%d requests after the TTL, want a second emoji.list", len(s.forms))
	}
}

func TestNoCustomEmojiIsAnEmptyList(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"emoji.list": {"": `{"ok":true}`}})

	got, err := api.Emoji(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("Emoji() = %#v, %v; want an empty map", got, err)
	}
}

func TestEmojiFailuresAreNotKept(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"emoji.list": {"": `{"ok":false,"error":"missing_scope"}`}})

	for range 2 {
		if _, err := api.Emoji(context.Background()); !HasCode(err, "missing_scope") {
			t.Fatalf("Emoji() err = %v, want missing_scope", err)
		}
	}
	if len(s.forms) != 2 {
		t.Errorf("%d requests, want each call to try", len(s.forms))
	}
}

func TestEmojiNeedsTheCustomEmojiCapability(t *testing.T) {
	api := NewWebAPI("http://127.0.0.1:1/", http.DefaultClient, func(url.Values, http.Header) {}, domain.Capabilities{}, slog.Default())

	if _, err := api.Emoji(context.Background()); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("Emoji() err = %v, want ErrUnsupported", err)
	}
}

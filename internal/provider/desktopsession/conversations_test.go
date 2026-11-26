package desktopsession

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

func TestConversationsJoinTheListWithClientCounts(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/users.conversations": jsonBody(http.StatusOK, string(fixture(t, "users.conversations.json"))),
		"/api/client.counts":       jsonBody(http.StatusOK, string(fixture(t, "client.counts.json"))),
	})
	p, logs := f.provider(t)

	got, err := p.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	conv := func(s string) domain.ConversationID { return parse(t, domain.ParseConversationID, s) }
	ts := func(s string) domain.TS { return parse(t, domain.ParseTS, s) }
	want := []domain.Conversation{
		{ID: conv("C0001"), Kind: domain.Channel, Name: "general",
			Latest: ts("1700000300.000300"), LastRead: ts("1700000100.000100"), Mentions: 2},
		{ID: conv("C0002"), Kind: domain.PrivateChannel, Name: "secret",
			Latest: ts("1700000400.000400"), LastRead: ts("1700000400.000400")},
		// Its counts entry does not parse: the read state stays unknown.
		{ID: conv("C0003"), Kind: domain.Channel, Name: "quiet"},
		// Never read: Slack's zero marker is the zero TS.
		{ID: conv("G0001"), Kind: domain.GroupMessage, Name: "mpdm-alice--bob--me-1",
			Latest: ts("1700000500.000500"), Mentions: 1},
		{ID: conv("D0001"), Kind: domain.DirectMessage,
			Latest: ts("1700000700.000700"), LastRead: ts("1700000600.000600"), Mentions: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Conversations() =\n%+v\nwant\n%+v", got, want)
	}
	if !strings.Contains(logs.String(), "C0003") {
		t.Errorf("logs = %q, want the dropped counts entry named", logs)
	}
	for _, req := range f.requests() {
		if req.Form.Get("token") != testToken || req.Cookie != testCookie {
			t.Errorf("%s was not signed like the web client", req.Path)
		}
	}
}

func TestCountsEntriesWithABadFieldAreRejected(t *testing.T) {
	for name, tc := range map[string]struct {
		entry countsEntry
		err   error
	}{
		"bad id":        {countsEntry{ID: "U0001"}, domain.ErrInvalidID},
		"bad latest":    {countsEntry{ID: "C0001", Latest: "x"}, domain.ErrInvalidTS},
		"bad last_read": {countsEntry{ID: "C0001", Latest: "1700000100.000100", LastRead: "x"}, domain.ErrInvalidTS},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := tc.entry.parse()
			if !errors.Is(err, tc.err) || got != (domain.Conversation{}) {
				t.Fatalf("parse() = %+v, %v; want nothing and %v", got, err, tc.err)
			}
		})
	}
}

func TestConversationsFailWhenEitherCallFails(t *testing.T) {
	list := jsonBody(http.StatusOK, string(fixture(t, "users.conversations.json")))
	counts := jsonBody(http.StatusOK, string(fixture(t, "client.counts.json")))
	revoked := jsonBody(http.StatusOK, `{"ok":false,"error":"invalid_auth"}`)
	for name, routes := range map[string]map[string]http.HandlerFunc{
		"users.conversations": {"/api/users.conversations": revoked, "/api/client.counts": counts},
		"client.counts":       {"/api/users.conversations": list, "/api/client.counts": revoked},
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := newFakeSlack(t, routes).provider(t)

			got, err := p.Conversations(context.Background())
			if got != nil || !errors.Is(err, provider.ErrAuthExpired) || !strings.HasPrefix(err.Error(), name+": ") {
				t.Fatalf("Conversations() = %v, %v; want nil and a %s ErrAuthExpired", got, err, name)
			}
		})
	}
}

package internalapp

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

func TestConversationsListsEveryKindWithoutReadState(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{"users.conversations": okJSON(readFixture(t, "users.conversations.json"))})
	p, _ := newTestProvider(t, f)

	got, err := p.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// users.conversations carries no latest, last_read or mentions: they
	// stay zero, and the sync engine reads the markers with LastRead.
	want := []domain.Conversation{
		{ID: mustConv(t, "C0001"), Kind: domain.Channel, Name: "general"},
		{ID: mustConv(t, "C0002"), Kind: domain.PrivateChannel, Name: "secret"},
		{ID: mustConv(t, "G0001"), Kind: domain.GroupMessage, Name: "mpdm-alice--bob--me-1"},
		{ID: mustConv(t, "D0001"), Kind: domain.DirectMessage, User: mustUser(t, "U0002")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Conversations() =\n%+v\nwant\n%+v", got, want)
	}
	if calls := f.recorded(); len(calls) != 1 || calls[0].Auth != "Bearer "+testUserToken {
		t.Errorf("calls = %+v, want one users.conversations with the user token", calls)
	}
}

func TestLastReadReadsTheMarkerFromConversationsInfo(t *testing.T) {
	for fixture, want := range map[string]string{
		"conversations.info.json":            "1700000500.000500",
		"conversations.info.never_read.json": "",
	} {
		t.Run(fixture, func(t *testing.T) {
			f := newFakeSlack(t, map[string]http.HandlerFunc{"conversations.info": okJSON(readFixture(t, fixture))})
			p, _ := newTestProvider(t, f)

			got, err := p.LastRead(context.Background(), mustConv(t, "C0001"))
			if err != nil {
				t.Fatal(err)
			}

			if (want == "" && !got.IsZero()) || (want != "" && got != mustTS(t, want)) {
				t.Errorf("LastRead() = %v, want %q", got, want)
			}
			if calls := f.recorded(); len(calls) != 1 || calls[0].Form.Get("channel") != "C0001" {
				t.Errorf("calls = %+v, want one conversations.info for C0001", calls)
			}
		})
	}
}

func TestConversationStateReadsWhatConversationsInfoReports(t *testing.T) {
	for fixture, want := range map[string]domain.Conversation{
		// A DM: Slack sends the latest message and the unread count.
		"conversations.info.im.json": {ID: mustConv(t, "D0001"), Kind: domain.DirectMessage, User: mustUser(t, "U0002"),
			Latest: mustTS(t, "1700000700.000700"), LastRead: mustTS(t, "1700000500.000500"), Mentions: 2},
		// A channel: only the marker. The latest stays zero (unknown), and
		// the unread count is not a mention count.
		"conversations.info.json": {ID: mustConv(t, "C0001"), Kind: domain.Channel, Name: "general",
			LastRead: mustTS(t, "1700000500.000500")},
	} {
		t.Run(fixture, func(t *testing.T) {
			f := newFakeSlack(t, map[string]http.HandlerFunc{"conversations.info": okJSON(readFixture(t, fixture))})
			p, _ := newTestProvider(t, f)

			got, err := p.ConversationState(context.Background(), want.ID)
			if err != nil {
				t.Fatal(err)
			}

			if got != want {
				t.Errorf("ConversationState() =\n%+v\nwant\n%+v", got, want)
			}
			if calls := f.recorded(); len(calls) != 1 || calls[0].Form.Get("channel") != want.ID.String() {
				t.Errorf("calls = %+v, want one conversations.info for %v", calls, want.ID)
			}
		})
	}
}

func TestConversationStateFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		body  string
		check func(error) bool
	}{
		"rejected token": {`{"ok":false,"error":"token_revoked"}`, func(err error) bool { return errors.Is(err, provider.ErrAuthExpired) }},
		"bad latest": {`{"ok":true,"channel":{"id":"D0001","is_im":true,"latest":{"ts":"soon"}}}`, func(err error) bool {
			return errors.Is(err, domain.ErrInvalidTS) && strings.HasPrefix(err.Error(), "conversations.info: ")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeSlack(t, map[string]http.HandlerFunc{"conversations.info": okJSON([]byte(tc.body))})
			p, _ := newTestProvider(t, f)

			got, err := p.ConversationState(context.Background(), mustConv(t, "D0001"))
			if got != (domain.Conversation{}) || !tc.check(err) {
				t.Fatalf("ConversationState() = %+v, %v", got, err)
			}
		})
	}
}

func TestLastReadFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		body  string
		check func(error) bool
	}{
		"rejected token": {`{"ok":false,"error":"token_revoked"}`, func(err error) bool { return errors.Is(err, provider.ErrAuthExpired) }},
		"bad marker": {`{"ok":true,"channel":{"id":"C0001","last_read":"soon"}}`, func(err error) bool {
			return errors.Is(err, domain.ErrInvalidTS) && strings.HasPrefix(err.Error(), "conversations.info: ")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeSlack(t, map[string]http.HandlerFunc{"conversations.info": okJSON([]byte(tc.body))})
			p, _ := newTestProvider(t, f)

			got, err := p.LastRead(context.Background(), mustConv(t, "C0001"))
			if !got.IsZero() || !tc.check(err) {
				t.Fatalf("LastRead() = %v, %v", got, err)
			}
		})
	}
}

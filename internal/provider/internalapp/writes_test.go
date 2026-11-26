package internalapp

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

func TestEditReturnsTheEditedMessage(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{"chat.update": okJSON(readFixture(t, "chat.update.json"))})
	p, _ := newTestProvider(t, f)
	ts := mustTS(t, "1700000500.000500")

	got, err := p.Edit(context.Background(), mustConv(t, "C0001"), ts, "hi there, edited")
	if err != nil {
		t.Fatal(err)
	}

	want := domain.Message{TS: ts, Author: mustAuthor(t, "U0001"), Text: "hi there, edited", Edited: mustTS(t, "1700000600.000000")}
	if got != want {
		t.Fatalf("Edit() = %+v, want %+v", got, want)
	}
	c := f.recorded()[0]
	if c.Auth != "Bearer "+testUserToken || c.Form.Get("channel") != "C0001" || c.Form.Get("ts") != ts.String() || c.Form.Get("text") != "hi there, edited" {
		t.Errorf("call = %+v", c)
	}
}

func TestWritesReachTheirMethods(t *testing.T) {
	ok := okJSON([]byte(`{"ok":true}`))
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"chat.delete": ok, "reactions.add": ok, "reactions.remove": ok,
		"users.setPresence": ok, "dnd.setSnooze": ok, "dnd.endSnooze": ok,
	})
	p, _ := newTestProvider(t, f)
	ctx, conv, ts := context.Background(), mustConv(t, "C0001"), mustTS(t, "1700000500.000500")

	for _, err := range []error{
		p.Delete(ctx, conv, ts),
		p.AddReaction(ctx, conv, ts, "+1"),
		p.RemoveReaction(ctx, conv, ts, "+1"),
		p.SetPresence(ctx, true),
		p.SnoozeDND(ctx, 60),
		p.EndDND(ctx),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}

	var methods []string
	for _, c := range f.recorded() {
		methods = append(methods, c.Method)
	}
	want := []string{"chat.delete", "reactions.add", "reactions.remove", "users.setPresence", "dnd.setSnooze", "dnd.endSnooze"}
	if !reflect.DeepEqual(methods, want) {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
	if got := f.recorded()[3].Form.Get("presence"); got != "away" {
		t.Errorf("presence = %q, want away", got)
	}
}

func TestSearchMessagesReturnsHitsWithTheirConversation(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{"search.messages": okJSON(readFixture(t, "search.messages.json"))})
	p, _ := newTestProvider(t, f)

	got, err := p.SearchMessages(context.Background(), "deploy", 20)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.SearchHit{
		{Conversation: mustConv(t, "C0001"), Message: domain.Message{TS: mustTS(t, "1700000200.000200"), Author: mustAuthor(t, "U0002"), Text: "deploy done"}},
		{Conversation: mustConv(t, "D0001"), Message: domain.Message{TS: mustTS(t, "1700000100.000100"), Author: mustAuthor(t, "U0001"), Text: "deploy now?"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchMessages() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestSearchUsersSearchesTheDirectory(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{"users.list": okJSON(readFixture(t, "users.list.json"))})
	p, _ := newTestProvider(t, f)

	got, err := p.SearchUsers(context.Background(), "ana", 10)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.User{
		{ID: mustUser(t, "U0001"), Name: "ana", RealName: "Ana Lopez", DisplayName: "anita"},
		{ID: mustUser(t, "U0003"), Name: "juana", RealName: "Juana Diaz"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchUsers() = %+v, want %+v", got, want)
	}
}

func TestWritesReportSlackErrorsTyped(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"chat.update":     okJSON([]byte(`{"ok":false,"error":"invalid_auth"}`)),
		"chat.delete":     okJSON([]byte(`{"ok":false,"error":"message_not_found"}`)),
		"search.messages": respond(http.StatusTooManyRequests, map[string]string{"Retry-After": "20"}, []byte(`{}`)),
	})
	p, _ := newTestProvider(t, f)
	ctx, conv, ts := context.Background(), mustConv(t, "C0001"), mustTS(t, "1700000500.000500")

	if _, err := p.Edit(ctx, conv, ts, "x"); !errors.Is(err, provider.ErrAuthExpired) {
		t.Errorf("Edit() err = %v, want ErrAuthExpired", err)
	}
	if err := p.Delete(ctx, conv, ts); !slackwire.HasCode(err, "message_not_found") || errors.Is(err, provider.ErrAuthExpired) {
		t.Errorf("Delete() err = %v, want a plain message_not_found", err)
	}
	var limited *provider.RateLimitedError
	if _, err := p.SearchMessages(ctx, "x", 5); !errors.As(err, &limited) || limited.RetryAfter != 20*time.Second {
		t.Errorf("SearchMessages() err = %v, want a 20s RateLimitedError", err)
	}
}

package desktopsession

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

func fixtureBody(t *testing.T, name string) http.HandlerFunc {
	return jsonBody(http.StatusOK, string(fixture(t, name)))
}

func TestWritesAreSignedLikeTheWebClient(t *testing.T) {
	ok := jsonBody(http.StatusOK, `{"ok":true}`)
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/chat.update": fixtureBody(t, "chat.update.json"), "/api/chat.delete": ok,
		"/api/reactions.add": ok, "/api/reactions.remove": ok, "/api/users.setPresence": ok,
		"/api/dnd.setSnooze": ok, "/api/dnd.endSnooze": ok,
	})
	p, _ := f.provider(t)
	ctx, conv, ts := context.Background(), parse(t, domain.ParseConversationID, "C0001"), parse(t, domain.ParseTS, "1700000500.000500")

	edited, err := p.Edit(ctx, conv, ts, "hi there, edited")
	for _, e := range []error{
		err,
		p.Delete(ctx, conv, ts),
		p.AddReaction(ctx, conv, ts, "smile::skin-tone-2"),
		p.RemoveReaction(ctx, conv, ts, "smile::skin-tone-2"),
		p.SetPresence(ctx, false),
		p.SnoozeDND(ctx, 15),
		p.EndDND(ctx),
	} {
		if e != nil {
			t.Fatal(e)
		}
	}

	if edited.Edited != parse(t, domain.ParseTS, "1700000600.000000") || edited.Text != "hi there, edited" {
		t.Errorf("Edit() = %+v, want the stored text and edited ts", edited)
	}
	reqs := f.requests()
	if len(reqs) != 7 {
		t.Fatalf("%d requests, want 7", len(reqs))
	}
	for _, r := range reqs {
		if r.Form.Get("token") != testToken || r.Cookie != testCookie {
			t.Errorf("%s: token %q, cookie %q; want the session's", r.Path, r.Form.Get("token"), r.Cookie)
		}
		assertBrowserHeaders(t, r.Header)
	}
	if got := reqs[2].Form.Get("name"); got != "smile::skin-tone-2" {
		t.Errorf("reaction name = %q", got)
	}
	if got := reqs[4].Form.Get("presence"); got != "auto" {
		t.Errorf("presence = %q, want auto", got)
	}
}

func TestSearchesParseIntoDomainTypes(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/search.messages": fixtureBody(t, "search.messages.json"),
		"/api/users.list":      fixtureBody(t, "users.list.json"),
	})
	p, _ := f.provider(t)

	hits, err := p.SearchMessages(context.Background(), "deploy", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Conversation != parse(t, domain.ParseConversationID, "C0001") || hits[1].Message.Text != "deploy now?" {
		t.Errorf("SearchMessages() = %+v", hits)
	}
	users, err := p.SearchUsers(context.Background(), "@juana", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.User{{ID: parse(t, domain.ParseUserID, "U0003"), Name: "juana", RealName: "Juana Diaz"}}
	if !reflect.DeepEqual(users, want) {
		t.Errorf("SearchUsers() = %+v, want %+v", users, want)
	}
}

func TestWriteFailuresKeepTheirKind(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/reactions.add": jsonBody(http.StatusOK, `{"ok":false,"error":"token_revoked"}`),
		"/api/chat.update":   jsonBody(http.StatusOK, `{"ok":false,"error":"cant_update_message"}`),
		"/api/dnd.setSnooze": jsonBody(http.StatusOK, `{"ok":false,"error":"invalid_auth"}`),
		"/api/users.setPresence": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "3")
			jsonBody(http.StatusTooManyRequests, `{}`)(w, r)
		},
	})
	p, _ := f.provider(t)
	ctx, conv, ts := context.Background(), parse(t, domain.ParseConversationID, "C0001"), parse(t, domain.ParseTS, "1700000500.000500")

	if err := p.AddReaction(ctx, conv, ts, "+1"); !errors.Is(err, provider.ErrAuthExpired) {
		t.Errorf("AddReaction() err = %v, want ErrAuthExpired", err)
	}
	if err := p.SnoozeDND(ctx, 10); !errors.Is(err, provider.ErrAuthExpired) {
		t.Errorf("SnoozeDND() err = %v, want ErrAuthExpired", err)
	}
	if _, err := p.Edit(ctx, conv, ts, "x"); !slackwire.HasCode(err, "cant_update_message") || errors.Is(err, provider.ErrAuthExpired) {
		t.Errorf("Edit() err = %v, want a plain cant_update_message", err)
	}
	var limited *provider.RateLimitedError
	if err := p.SetPresence(ctx, true); !errors.As(err, &limited) || limited.RetryAfter != 3*time.Second {
		t.Errorf("SetPresence() err = %v, want a 3s RateLimitedError", err)
	}
}

func TestNamesAndEmojiComeFromTheWebAPI(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/users.list": fixtureBody(t, "users.list.json"),
		"/api/emoji.list": jsonBody(http.StatusOK, `{"ok":true,"emoji":{"shipit":"alias:squirrel"}}`),
	})
	p, _ := f.provider(t)

	users, err := p.Users(context.Background(), []domain.UserID{parse(t, domain.ParseUserID, "U0003")})
	if err != nil || len(users) != 1 || users[0].Name != "juana" {
		t.Errorf("Users() = %+v, %v; want juana", users, err)
	}
	emoji, err := p.Emoji(context.Background())
	if err != nil || emoji["shipit"] != "alias:squirrel" {
		t.Errorf("Emoji() = %v, %v; want shipit", emoji, err)
	}
}

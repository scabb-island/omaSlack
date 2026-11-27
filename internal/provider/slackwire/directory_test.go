package slackwire

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

func userIDs(t *testing.T, ids ...string) []domain.UserID {
	out := make([]domain.UserID, len(ids))
	for i, id := range ids {
		out[i] = must(t, domain.ParseUserID, id)
	}
	return out
}

// withUsersInfo is directoryPages plus a users.info that answers body.
func withUsersInfo(body string) map[string]map[string]string {
	return map[string]map[string]string{"users.list": directoryPages["users.list"], "users.info": {"": body}}
}

// usersInfoCalls lists the users s was asked for with users.info, in
// order. The fake keeps forms, not paths; only users.info sends "user".
func usersInfoCalls(s *slack) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var asked []string
	for _, f := range s.forms {
		if f.Has("user") {
			asked = append(asked, f.Get("user"))
		}
	}
	return asked
}

func TestUsersAnswerFromTheDirectoryInTheOrderAsked(t *testing.T) {
	// Accented names are intentional: they come back unchanged.
	api, s, _ := fakeSlack(t, withUsersInfo(`{"ok":false,"error":"user_not_found"}`))

	got, err := api.Users(context.Background(), userIDs(t, "W0003", "U0001", "W0003", "U0002"))
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.User{
		user(t, "W0003", "juana", "Juana Ana Díaz", ""),
		user(t, "U0001", "ana", "Ana Pérez", "anita"),
		user(t, "U0002", "bob", "Bob Old", ""), // deleted: still named in old messages
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Users() =\n%+v\nwant\n%+v", got, want)
	}
	if len(s.forms) != 2 || len(usersInfoCalls(s)) != 0 {
		t.Errorf("%d requests, users.info for %v; want only the two pages of users.list", len(s.forms), usersInfoCalls(s))
	}
}

func TestUsersOutsideTheDirectoryAreLookedUpOnce(t *testing.T) {
	api, s, _ := fakeSlack(t, withUsersInfo(`{"ok":true,"user":{"id":"W0009","name":"guest","profile":{"real_name":"Guest Person"}}}`))
	ids := userIDs(t, "U0001", "W0009")

	for range 2 {
		got, err := api.Users(context.Background(), ids)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[1] != user(t, "W0009", "guest", "Guest Person", "") {
			t.Fatalf("Users() = %+v, want ana and the guest", got)
		}
	}
	if asked := usersInfoCalls(s); !reflect.DeepEqual(asked, []string{"W0009"}) {
		t.Errorf("users.info asked for %v, want W0009 once", asked)
	}
}

func TestUnknownUsersAreLeftOutAndNotAskedForAgain(t *testing.T) {
	api, s, _ := fakeSlack(t, withUsersInfo(`{"ok":false,"error":"user_not_found"}`))

	for range 2 {
		got, err := api.Users(context.Background(), userIDs(t, "W0404"))
		if err != nil || len(got) != 0 {
			t.Fatalf("Users() = %+v, %v; want nobody", got, err)
		}
	}
	if asked := usersInfoCalls(s); len(asked) != 1 {
		t.Errorf("users.info asked for %v, want W0404 once", asked)
	}
}

func TestOneCallLooksUpAtMostMaxLookups(t *testing.T) {
	api, s, _ := fakeSlack(t, withUsersInfo(`{"ok":false,"error":"user_not_visible"}`))
	var ids []domain.UserID
	for i := range maxLookups + 5 {
		ids = append(ids, must(t, domain.ParseUserID, fmt.Sprintf("W9%03d", i)))
	}

	for range 2 {
		if _, err := api.Users(context.Background(), ids); err != nil {
			t.Fatal(err)
		}
	}
	if asked := usersInfoCalls(s); len(asked) != maxLookups+5 {
		t.Errorf("users.info asked %d times over two calls, want %d then the 5 left", len(asked), maxLookups+5)
	}
}

func TestUsersFailWhenALookupFails(t *testing.T) {
	api, _, _ := fakeSlack(t, withUsersInfo(`{"ok":false,"error":"invalid_auth"}`))

	if _, err := api.Users(context.Background(), userIDs(t, "U0001", "W0009")); !errors.Is(err, provider.ErrAuthExpired) {
		t.Errorf("Users() err = %v, want ErrAuthExpired", err)
	}
}

func TestUsersFailWhenTheDirectoryCannotLoad(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"users.list": {"": `{"ok":false,"error":"ratelimited"}`}})

	if _, err := api.Users(context.Background(), userIDs(t, "U0001")); err == nil {
		t.Error("Users() err = nil, want the users.list error")
	}
}

func TestLookedUpUsersAreForgottenWithTheDirectory(t *testing.T) {
	api, s, _ := fakeSlack(t, withUsersInfo(`{"ok":false,"error":"user_not_found"}`))
	now := time.Unix(1700000000, 0)
	api.now = func() time.Time { return now }
	ask := func() {
		t.Helper()
		if _, err := api.Users(context.Background(), userIDs(t, "W0404")); err != nil {
			t.Fatal(err)
		}
	}

	ask()
	now = now.Add(directoryTTL)
	ask()
	if asked := usersInfoCalls(s); len(asked) != 2 {
		t.Errorf("users.info asked for %v, want W0404 again once the directory reloaded", asked)
	}
}

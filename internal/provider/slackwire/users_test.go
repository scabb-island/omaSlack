package slackwire

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// directoryPages is a two-page users.list: a deleted user and one whose id
// does not parse are in it too.
var directoryPages = map[string]map[string]string{"users.list": {
	"": `{"ok":true,"members":[
		{"id":"U0001","name":"ana","real_name":"Ana Pérez","profile":{"real_name":"Ana Pérez","display_name":"anita"}},
		{"id":"U0002","name":"bob","deleted":true,"profile":{"real_name":"Bob Old"}},
		{"id":"nope","name":"broken"}
	],"response_metadata":{"next_cursor":"page2"}}`,
	"page2": `{"ok":true,"members":[
		{"id":"W0003","name":"juana","profile":{"real_name":"Juana Ana Díaz","display_name":""}},
		{"id":"U0004","name":"deploybot","is_bot":true,"profile":{"real_name":"Deploy Bot"}}
	],"response_metadata":{"next_cursor":""}}`,
}}

func user(t *testing.T, id, name, realName, display string) domain.User {
	return domain.User{ID: must(t, domain.ParseUserID, id), Name: name, RealName: realName, DisplayName: display}
}

func TestSearchUsersMatchesHandlesAndNamesWithPrefixesFirst(t *testing.T) {
	// Accented names are intentional: matching must not depend on ASCII.
	api, s, logs := fakeSlack(t, directoryPages)
	ana := user(t, "U0001", "ana", "Ana Pérez", "anita")
	juana := user(t, "W0003", "juana", "Juana Ana Díaz", "")

	cases := map[string][]domain.User{
		"ana":    {ana, juana}, // ana starts with it, juana only contains it
		"@ANA":   {ana, juana},
		"díaz":   {juana},
		"anita":  {ana},
		"deploy": {user(t, "U0004", "deploybot", "Deploy Bot", "")},
		"bob":    {}, // deleted
		"  ":     {},
	}
	for query, want := range cases {
		got, err := api.SearchUsers(context.Background(), query, 10)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SearchUsers(%q) = %+v, want %+v", query, got, want)
		}
	}
	if len(s.forms) != 2 || s.form(0).Get("limit") != "200" || s.form(1).Get("cursor") != "page2" {
		t.Errorf("forms = %v, want the two pages of users.list once", s.forms)
	}
	if !strings.Contains(logs.String(), "nope") {
		t.Errorf("logs = %q, want the dropped user", logs.String())
	}
}

func TestSearchUsersStopsAtTheLimit(t *testing.T) {
	api, _, _ := fakeSlack(t, directoryPages)

	for limit, want := range map[int]int{1: 1, 0: 2, -1: 2} {
		got, err := api.SearchUsers(context.Background(), "ana", limit)
		if err != nil || len(got) != want {
			t.Errorf("SearchUsers(limit %d) = %d users, %v; want %d", limit, len(got), err, want)
		}
	}
}

func TestTheDirectoryIsReloadedOnceItIsStale(t *testing.T) {
	api, s, _ := fakeSlack(t, directoryPages)
	now := time.Unix(1700000000, 0)
	api.now = func() time.Time { return now }

	search := func() {
		t.Helper()
		if _, err := api.SearchUsers(context.Background(), "ana", 5); err != nil {
			t.Fatal(err)
		}
	}
	search()
	now = now.Add(directoryTTL - time.Second)
	search()
	if len(s.forms) != 2 {
		t.Fatalf("%d requests before the TTL, want the 2 pages of one load", len(s.forms))
	}
	now = now.Add(time.Second)
	search()
	if len(s.forms) != 4 {
		t.Errorf("%d requests after the TTL, want a second load", len(s.forms))
	}
}

func TestSearchUsersFailsWhenTheDirectoryCannotLoad(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"users.list": {
		"":      directoryPages["users.list"][""],
		"page2": `{"ok":false,"error":"invalid_auth"}`,
	}})

	if _, err := api.SearchUsers(context.Background(), "ana", 5); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("SearchUsers() err = %v, want ErrAuthExpired", err)
	}
	if _, err := api.SearchUsers(context.Background(), "ana", 5); err == nil {
		t.Fatal("second SearchUsers() err = nil; a failed load must not be cached")
	}
	if len(s.forms) != 4 {
		t.Errorf("%d requests, want both searches to try the load", len(s.forms))
	}
}

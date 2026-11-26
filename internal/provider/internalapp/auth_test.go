package internalapp

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/provider"
)

func TestAuthTestNamesTheWorkspace(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"auth.test": okJSON([]byte(`{"ok":true,"url":"https://acme.slack.com/","team":"Acme","team_id":"T0ACME"}`)),
	})
	got, err := AuthTest(t.Context(), testCreds(), WithAPIURL(f.apiURL()))
	want := Identity{Team: mustTeam(t, "T0ACME"), Name: "Acme", URL: "https://acme.slack.com/"}
	if err != nil || got != want {
		t.Fatalf("AuthTest = %v, %v; want %v", got, err, want)
	}
	if calls := f.recorded(); len(calls) != 1 || calls[0].Auth != "Bearer "+testUserToken {
		t.Errorf("calls = %v, want one with the user token", calls)
	}
}

func TestAuthTestFailures(t *testing.T) {
	for name, tt := range map[string]struct {
		handler http.HandlerFunc
		want    error
	}{
		"revoked token":   {okJSON([]byte(`{"ok":false,"error":"token_revoked"}`)), provider.ErrAuthExpired},
		"invalid team ID": {okJSON([]byte(`{"ok":true,"team_id":"nope"}`)), nil},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeSlack(t, map[string]http.HandlerFunc{"auth.test": tt.handler})
			_, err := AuthTest(t.Context(), testCreds(), WithAPIURL(f.apiURL()))
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), testUserToken) {
				t.Errorf("the error carries the token: %v", err)
			}
		})
	}
}

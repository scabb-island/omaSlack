package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession"
	"gitlab.com/scabb-island/omaslack/internal/provider/internalapp"
	"gitlab.com/scabb-island/omaslack/internal/provider/providertest"
)

func mustTeam(t *testing.T, s string) domain.TeamID {
	t.Helper()
	id, err := domain.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustUser(t *testing.T, s string) domain.UserID {
	t.Helper()
	id, err := domain.ParseUserID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// slackServer answers auth.test with body.
func slackServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth.test" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/"
}

const authTestOK = `{"ok":true,"team":"Acme","team_id":"T0ACME","user_id":"U0ME"}`

func TestAuthTestNamesTheWorkspaceAndTheUserOfBothProviders(t *testing.T) {
	api := slackServer(t, authTestOK)
	team := mustTeam(t, "T0ACME")
	for _, p := range []provider.Provider{
		internalapp.New(team, keyring.InternalApp{UserToken: "xoxp-1", AppToken: "xapp-1"}, internalapp.WithAPIURL(api)),
		desktopsession.New(team, keyring.DesktopSession{Token: "xoxc-1", Cookie: "xoxd-1"}, desktopsession.WithAPIURL(api)),
	} {
		got, err := AuthTest(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		if want := (Identity{Name: "Acme", Self: mustUser(t, "U0ME")}); got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	}
}

func TestAuthTestReportsRejectedCredentials(t *testing.T) {
	api := slackServer(t, `{"ok":false,"error":"invalid_auth"}`)
	p := internalapp.New(mustTeam(t, "T0ACME"), keyring.InternalApp{UserToken: "xoxp-1"}, internalapp.WithAPIURL(api))
	if _, err := AuthTest(context.Background(), p); !errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("err = %v, want ErrAuthExpired", err)
	}
}

func TestAuthTestRejectsAMalformedUser(t *testing.T) {
	api := slackServer(t, `{"ok":true,"team":"Acme","user_id":"nope"}`)
	p := internalapp.New(mustTeam(t, "T0ACME"), keyring.InternalApp{UserToken: "xoxp-1"}, internalapp.WithAPIURL(api))
	if _, err := AuthTest(context.Background(), p); err == nil {
		t.Fatal("no error for a malformed user ID")
	}
}

func TestAuthTestNeedsAWebAPIProvider(t *testing.T) {
	p := providertest.New(mustTeam(t, "T0ACME"), domain.Capabilities{})
	if _, err := AuthTest(context.Background(), p); err == nil {
		t.Fatal("no error for a provider without the Web API")
	}
}

// identifyAs is an Identifier that always answers id, err.
func identifyAs(id Identity, err error) Identifier {
	return func(context.Context, provider.Provider) (Identity, error) { return id, err }
}

func TestIdentifyRemembersWhatSlackAnswered(t *testing.T) {
	cache := identityCache{dir: t.TempDir()}
	team := mustTeam(t, "T0ACME")
	acme := Identity{Name: "Acme", Self: mustUser(t, "U0ME")}

	if got := identify(context.Background(), identifyAs(acme, nil), cache, team, nil, slog.New(slog.DiscardHandler)); got != acme {
		t.Fatalf("online: got %+v", got)
	}
	offline := identifyAs(Identity{}, errors.New("no network"))
	if got := identify(context.Background(), offline, cache, team, nil, slog.New(slog.DiscardHandler)); got != acme {
		t.Fatalf("offline: got %+v, want the remembered %+v", got, acme)
	}
}

func TestIdentifyFallsBackToTheTeamID(t *testing.T) {
	cache := identityCache{dir: t.TempDir()}
	team := mustTeam(t, "T0ACME")
	if err := os.WriteFile(filepath.Join(cache.dir, "T0ACME.json"), []byte(`{"self":"bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := identify(context.Background(), identifyAs(Identity{}, provider.ErrAuthExpired), cache, team, nil, slog.New(slog.DiscardHandler))
	if got != (Identity{Name: "T0ACME"}) {
		t.Fatalf("got %+v, want only the team ID as name", got)
	}
}

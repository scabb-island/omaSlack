package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

func TestLocalSourcesGathersInstalledSources(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir()) // no Slack desktop app, no ~/.config browsers
	t.Setenv("TMPDIR", t.TempDir())
	// One signed-in Brave profile.
	writeLevelDB(t, filepath.Join(cfg, "BraveSoftware", "Brave-Browser", "Default", "Local Storage", "leveldb"),
		append([]byte{0x01}, `{"teams":{"T06M3KK7Y":{"token":"xoxc-x"}}}`...))
	if got := LocalSources(); len(got) != 1 {
		t.Fatalf("LocalSources returned %d sources, want 1 (Brave only)", len(got))
	}
}

// fakeSource is a session source with fixed workspaces.
type fakeSource struct {
	ws  []Workspace
	err error
}

func (f fakeSource) Workspaces(context.Context) ([]Workspace, error) { return f.ws, f.err }

func refreshTeam(t *testing.T) domain.TeamID { return mustTeam(t, "T0RSWNW0G") }

// acceptsToken is an AuthTest that accepts only the given token.
func acceptsToken(token string) AuthTest {
	return func(_ context.Context, creds keyring.DesktopSession) (Identity, error) {
		if creds.Token == token {
			return Identity{TeamID: "T0RSWNW0G", Team: "A3Mercados"}, nil
		}
		return Identity{}, errors.New("invalid_auth")
	}
}

func TestRefreshSessionReturnsTheFirstValidSource(t *testing.T) {
	team := refreshTeam(t)
	good := keyring.DesktopSession{Token: "xoxc-fresh", Cookie: "xoxd-c"}
	sources := []Source{
		fakeSource{err: errors.New("desktop app not readable")},
		fakeSource{ws: []Workspace{
			{Team: mustTeam(t, "T06M3KK7Y"), Credentials: keyring.DesktopSession{Token: "other"}},
			{Team: team, Credentials: good},
		}},
	}
	got, err := RefreshSession(context.Background(), team, sources, acceptsToken("xoxc-fresh"))
	if err != nil || got.Token != "xoxc-fresh" {
		t.Fatalf("RefreshSession = %+v, %v", got, err)
	}
}

func TestRefreshSessionSkipsStaleAndSignedOut(t *testing.T) {
	team := refreshTeam(t)
	sources := []Source{
		// Signed out for this team (no credentials): skipped.
		fakeSource{ws: []Workspace{{Team: team}}},
		// A stale session that auth.test rejects.
		fakeSource{ws: []Workspace{{Team: team, Credentials: keyring.DesktopSession{Token: "xoxc-stale"}}}},
		// A fresh one.
		fakeSource{ws: []Workspace{{Team: team, Credentials: keyring.DesktopSession{Token: "xoxc-fresh"}}}},
	}
	got, err := RefreshSession(context.Background(), team, sources, acceptsToken("xoxc-fresh"))
	if err != nil || got.Token != "xoxc-fresh" {
		t.Fatalf("RefreshSession = %+v, %v", got, err)
	}
}

func TestRefreshSessionNotSignedInWhenNoSourceValid(t *testing.T) {
	team := refreshTeam(t)
	sources := []Source{
		fakeSource{ws: []Workspace{{Team: team, Credentials: keyring.DesktopSession{Token: "xoxc-stale"}}}},
	}
	if _, err := RefreshSession(context.Background(), team, sources, acceptsToken("xoxc-fresh")); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("RefreshSession error = %v, want ErrNotSignedIn", err)
	}
}

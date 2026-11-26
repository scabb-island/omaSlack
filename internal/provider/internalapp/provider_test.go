package internalapp

import (
	"slices"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

var _ provider.Provider = (*Provider)(nil)

func mustTeam(t *testing.T, s string) domain.TeamID {
	t.Helper()
	id, err := domain.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTeamIsTheOneGiven(t *testing.T) {
	team := mustTeam(t, "T0001")
	p := New(team, keyring.InternalApp{UserToken: "xoxp-test", AppToken: "xapp-test"})
	if p.Team() != team {
		t.Fatalf("Team() = %v, want %v", p.Team(), team)
	}
}

func TestCapabilitiesAreWhatTheEventsAPIOffersAUserToken(t *testing.T) {
	p := New(mustTeam(t, "T0001"), keyring.InternalApp{})
	want := []domain.Capability{domain.Realtime, domain.Search, domain.Files, domain.PresenceWrite, domain.CustomEmoji}
	if got := p.Capabilities().List(); !slices.Equal(got, want) {
		t.Fatalf("Capabilities() = %v, want %v", got, want)
	}
}

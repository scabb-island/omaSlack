package desktopsession

import (
	"slices"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

func TestTeamIsTheOneGiven(t *testing.T) {
	team := parse(t, domain.ParseTeamID, "T0001")
	if got := New(team, keyring.DesktopSession{}).Team(); got != team {
		t.Fatalf("Team() = %v, want %v", got, team)
	}
}

func TestCapabilitiesAreEverythingTheWebClientCanDo(t *testing.T) {
	p := New(parse(t, domain.ParseTeamID, "T0001"), keyring.DesktopSession{})
	want := []domain.Capability{
		domain.Realtime, domain.Search, domain.Files, domain.PresenceWrite,
		domain.Typing, domain.CustomEmoji, domain.Sections,
	}
	if got := p.Capabilities().List(); !slices.Equal(got, want) {
		t.Fatalf("Capabilities() = %v, want %v", got, want)
	}
}

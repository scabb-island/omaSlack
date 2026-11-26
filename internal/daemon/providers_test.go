package daemon

import (
	"log/slog"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession"
	"gitlab.com/scabb-island/omaslack/internal/provider/internalapp"
)

func TestTheCredentialsPickTheProvider(t *testing.T) {
	build := SlackProviders(desktopsession.DefaultAPIURL, slog.New(slog.DiscardHandler))
	team := mustTeam(t, "T0ACME")

	p, err := build(team, keyring.DesktopSession{Token: "xoxc-1", Cookie: "xoxd-1"})
	if _, ok := p.(*desktopsession.Provider); !ok || err != nil || p.Team() != team {
		t.Errorf("desktop session: %T, %v", p, err)
	}
	p, err = build(team, keyring.InternalApp{UserToken: "xoxp-1", AppToken: "xapp-1"})
	if _, ok := p.(*internalapp.Provider); !ok || err != nil || p.Team() != team {
		t.Errorf("internal app: %T, %v", p, err)
	}
}

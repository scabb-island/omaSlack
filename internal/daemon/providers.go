package daemon

import (
	"fmt"
	"log/slog"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession"
	"gitlab.com/scabb-island/omaslack/internal/provider/internalapp"
)

// ProviderFactory builds the provider of team from its stored credentials.
type ProviderFactory func(team domain.TeamID, creds keyring.Credentials) (provider.Provider, error)

// SlackProviders is the ProviderFactory that reaches Slack: the
// credentials' kind picks the provider, and both log to log.
func SlackProviders(log *slog.Logger) ProviderFactory {
	return func(team domain.TeamID, creds keyring.Credentials) (provider.Provider, error) {
		switch c := creds.(type) {
		case keyring.DesktopSession:
			return desktopsession.New(team, c, desktopsession.WithLogger(log)), nil
		case keyring.InternalApp:
			return internalapp.New(team, c, internalapp.WithLogger(log)), nil
		}
		return nil, fmt.Errorf("no provider for %s credentials", creds.Provider())
	}
}

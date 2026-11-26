// Package desktopsession is the desktop-session provider: a workspace
// reached with the xoxc token and the cookies of a Slack desktop app or
// browser session (spec slack-connection). Package auth finds those
// credentials; this package talks to Slack with them.
package desktopsession

import (
	"context"
	"net/url"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// DefaultAPIURL is Slack's Web API base. Method names are appended to it.
const DefaultAPIURL = slackwire.DefaultAPIURL

type authTestResponse struct {
	slackwire.Envelope
	TeamID string `json:"team_id"`
	Team   string `json:"team"`
	URL    string `json:"url"`
}

// AuthTest returns the auth.test call that auth.FromCurl needs, against
// the Web API at apiURL (DefaultAPIURL, or a test server). It is signed
// like every call of the provider: the whole cookie set, on the
// workspace's own domain when the credentials know it. A rejected session
// wraps provider.ErrAuthExpired. Errors never carry the secrets.
func AuthTest(apiURL string) auth.AuthTest {
	return func(ctx context.Context, creds keyring.DesktopSession) (auth.Identity, error) {
		var resp authTestResponse
		// Not nil: the session signs into the form.
		if err := New(domain.TeamID{}, creds, WithAPIURL(apiURL)).Call(ctx, "auth.test", url.Values{}, &resp); err != nil {
			return auth.Identity{}, err
		}
		return auth.Identity{TeamID: resp.TeamID, Team: resp.Team, URL: resp.URL}, nil
	}
}

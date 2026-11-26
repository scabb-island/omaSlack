// Package desktopsession is the desktop-session provider: a workspace
// reached with the xoxc token and xoxd "d" cookie of a Slack desktop app
// or browser session (spec slack-connection). Package auth finds those
// credentials; this package talks to Slack with them.
package desktopsession

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
)

// DefaultAPIURL is Slack's Web API base. Method names are appended to it.
const DefaultAPIURL = "https://slack.com/api/"

// authErrorCodes are the Slack error codes that reject the session.
var authErrorCodes = []string{"invalid_auth", "token_revoked", "account_inactive", "not_authed"}

type authTestResponse struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
	TeamID string `json:"team_id"`
	Team   string `json:"team"`
	URL    string `json:"url"`
}

// AuthTest returns the auth.test call that auth.FromCurl needs, against
// the Web API at apiURL (DefaultAPIURL, or a test server). A rejected
// session wraps provider.ErrAuthExpired. Errors never carry the secrets.
func AuthTest(apiURL string) auth.AuthTest {
	return func(ctx context.Context, creds keyring.DesktopSession) (auth.Identity, error) {
		resp, err := postAuthTest(ctx, apiURL, creds)
		if err != nil {
			return auth.Identity{}, fmt.Errorf("auth.test: %w", err)
		}
		return auth.Identity{TeamID: resp.TeamID, Team: resp.Team, URL: resp.URL}, nil
	}
}

func postAuthTest(ctx context.Context, apiURL string, creds keyring.DesktopSession) (authTestResponse, error) {
	form := url.Values{"token": {creds.Token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"auth.test", strings.NewReader(form.Encode()))
	if err != nil {
		return authTestResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "d="+creds.Cookie) // already URL-encoded, as Slack sends it
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return authTestResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return authTestResponse{}, fmt.Errorf("HTTP %s", res.Status)
	}
	var resp authTestResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return authTestResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return resp, slackError(resp)
}

// slackError turns a failed response into an error naming Slack's code.
func slackError(resp authTestResponse) error {
	switch {
	case resp.OK:
		return nil
	case slices.Contains(authErrorCodes, resp.Error):
		return fmt.Errorf("%s: %w", resp.Error, provider.ErrAuthExpired)
	default:
		return fmt.Errorf("%s", resp.Error)
	}
}

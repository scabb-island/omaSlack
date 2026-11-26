package internalapp

import (
	"context"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

// Identity is the workspace a user token belongs to.
type Identity struct {
	Team domain.TeamID
	Name string
	URL  string
}

type authTestResponse struct {
	envelope
	TeamID string `json:"team_id"`
	Team   string `json:"team"`
	URL    string `json:"url"`
}

// AuthTest asks Slack's auth.test which workspace creds' user token
// belongs to, which also checks the token. `omaslack auth add` calls it
// before it stores anything. Of opts, only WithAPIURL matters.
func AuthTest(ctx context.Context, creds keyring.InternalApp, opts ...Option) (Identity, error) {
	var resp authTestResponse
	if err := New(domain.TeamID{}, creds, opts...).call(ctx, "auth.test", nil, &resp); err != nil {
		return Identity{}, err
	}
	team, err := domain.ParseTeamID(resp.TeamID)
	if err != nil {
		return Identity{}, fmt.Errorf("auth.test: %w", err)
	}
	return Identity{Team: team, Name: resp.Team, URL: resp.URL}, nil
}

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// Identity is what a workspace is called and who is signed in to it. The
// keyring holds only credentials, so both come from Slack.
type Identity struct {
	Name string        // the workspace's display name
	Self domain.UserID // the signed-in user; zero when unknown
}

// Identifier asks Slack who p is signed in as.
type Identifier func(ctx context.Context, p provider.Provider) (Identity, error)

// webCaller is a provider that can call any Web API method: both real
// providers are, through the slackwire.WebAPI they embed.
type webCaller interface {
	Call(ctx context.Context, method string, form url.Values, out slackwire.Response) error
}

type authTestResponse struct {
	slackwire.Envelope
	Team   string `json:"team"`
	UserID string `json:"user_id"`
}

// AuthTest is the Identifier of the real providers: Slack's auth.test,
// which both the xoxp and the xoxc tokens may call. Rejected credentials
// wrap provider.ErrAuthExpired.
func AuthTest(ctx context.Context, p provider.Provider) (Identity, error) {
	c, ok := p.(webCaller)
	if !ok {
		return Identity{}, fmt.Errorf("the %T provider cannot call auth.test", p)
	}
	var resp authTestResponse
	// Not nil: the desktop session signs into the form.
	if err := c.Call(ctx, "auth.test", url.Values{}, &resp); err != nil {
		return Identity{}, err
	}
	self, err := domain.ParseUserID(resp.UserID)
	if err != nil {
		return Identity{}, fmt.Errorf("auth.test: %w", err)
	}
	return Identity{Name: resp.Team, Self: self}, nil
}

// identifyTimeout bounds auth.test at startup, so a daemon started before
// the network is up still serves its cache.
const identifyTimeout = 10 * time.Second

// identify returns team's identity: Slack's answer when there is one,
// otherwise the last one remembered, otherwise just the team ID as name.
// Without Self, the sync engine stores the user's own reactions only once
// the real-time stream echoes them.
func identify(ctx context.Context, ask Identifier, cache identityCache, team domain.TeamID, p provider.Provider, log *slog.Logger) Identity {
	ctx, cancel := context.WithTimeout(ctx, identifyTimeout)
	defer cancel()
	id, err := ask(ctx, p)
	if err == nil {
		if err := cache.save(team, id); err != nil {
			log.Warn("daemon: could not remember the workspace identity", "team", team, "err", err)
		}
		return id
	}
	if errors.Is(err, provider.ErrAuthExpired) {
		log.Warn("daemon: Slack rejected the credentials; sign in again with omaslack auth add", "team", team)
	} else {
		log.Warn("daemon: could not ask Slack who is signed in; using the last known identity", "team", team, "err", err)
	}
	if id, ok := cache.load(team); ok {
		return id
	}
	return Identity{Name: team.String()}
}

// identityCache remembers each workspace's Identity next to its store, as
// <dir>/<team>.json, for when Slack cannot be asked.
type identityCache struct{ dir string }

type identityRecord struct {
	Name string `json:"name"`
	Self string `json:"self"`
}

func (c identityCache) path(team domain.TeamID) string {
	return filepath.Join(c.dir, team.String()+".json")
}

// load returns the remembered identity; a missing or malformed file is
// none.
func (c identityCache) load(team domain.TeamID) (Identity, bool) {
	data, err := os.ReadFile(c.path(team))
	if err != nil {
		return Identity{}, false
	}
	var r identityRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return Identity{}, false
	}
	self, err := domain.ParseUserID(r.Self)
	if err != nil {
		return Identity{}, false
	}
	return Identity{Name: r.Name, Self: self}, true
}

// save writes id through a temporary file, so a crash never leaves half
// a record.
func (c identityCache) save(team domain.TeamID, id Identity) error {
	data, err := json.Marshal(identityRecord{Name: id.Name, Self: id.Self.String()})
	if err != nil {
		return err
	}
	tmp := c.path(team) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path(team))
}

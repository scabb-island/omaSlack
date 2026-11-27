package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// refreshClient is the name auth refresh gives the daemon in hello.
const refreshClient = "omaslack-auth-refresh"

// refreshSignedOutHelp is for a workspace whose source no longer holds a
// valid session: the fix is to sign in to Slack there again.
const refreshSignedOutHelp = `No source holds a valid session for %s any more. Sign in to Slack again in
your browser (or the Slack desktop app), then run: omaslack auth refresh %s
`

// authRefresh renews the desktop-session workspaces whose sessions Slack
// rejected, by re-reading them from their local source. With a team ID it
// refreshes that one; with none, every desktop-session workspace the
// daemon reports as auth_expired.
func authRefresh(ctx context.Context, env Env, args []string) error {
	team, err := parseRefreshArgs(args)
	if err != nil {
		return err
	}
	ks, err := openKeyring(ctx, env)
	if err != nil {
		return err
	}
	defer ks.Close()
	targets, err := refreshTargets(ctx, env, ks, team)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Fprintln(env.Stdout, "No desktop-session workspace needs refreshing.")
		return nil
	}
	return refreshEach(ctx, env, ks, targets)
}

func parseRefreshArgs(args []string) (domain.TeamID, error) {
	switch len(args) {
	case 0:
		return domain.TeamID{}, nil
	case 1:
		team, err := domain.ParseTeamID(args[0])
		if err != nil {
			return domain.TeamID{}, authUsageError("not a team ID: %q", args[0])
		}
		return team, nil
	default:
		return domain.TeamID{}, authUsageError("auth refresh takes at most one team ID")
	}
}

// refreshTargets is which workspaces to refresh: the named one (which must
// be a desktop-session workspace), or every desktop-session workspace the
// daemon reports as auth_expired.
func refreshTargets(ctx context.Context, env Env, ks keyring.Store, team domain.TeamID) ([]domain.TeamID, error) {
	if team != (domain.TeamID{}) {
		creds, err := ks.Get(ctx, team)
		if err != nil {
			return nil, fmt.Errorf("%s is not in the keyring: %w", team, err)
		}
		if _, ok := creds.(keyring.DesktopSession); !ok {
			return nil, fmt.Errorf("%s uses the %s provider, which has its own tokens and is not refreshed this way", team, creds.Provider())
		}
		return []domain.TeamID{team}, nil
	}
	return expiredDesktopSessions(ctx, env)
}

// expiredDesktopSessions asks the daemon which desktop-session workspaces
// are auth_expired. It needs the daemon, since only it knows the states.
func expiredDesktopSessions(ctx context.Context, env Env) ([]domain.TeamID, error) {
	c, err := dialDaemon(ctx, env, refreshClient)
	if err != nil {
		return nil, fmt.Errorf("cannot ask the daemon which workspaces expired (name a workspace instead: omaslack auth refresh <team-id>): %w", err)
	}
	defer c.Close()
	raw, err := c.Call(ctx, "workspaces.list", nil)
	if err != nil {
		return nil, err
	}
	var workspaces []wire.Workspace
	if err := json.Unmarshal(raw, &workspaces); err != nil {
		return nil, err
	}
	var targets []domain.TeamID
	for _, w := range workspaces {
		if w.State != "auth_expired" || w.Provider != "desktop-session" {
			continue
		}
		if team, err := domain.ParseTeamID(w.Team); err == nil {
			targets = append(targets, team)
		}
	}
	return targets, nil
}

// refreshEach re-reads each target's session from its local source,
// validates it, stores it and, when any was refreshed, asks the daemon to
// reconnect. A target whose source has no valid session is reported and
// skipped; it stays auth_expired.
func refreshEach(ctx context.Context, env Env, ks keyring.Store, targets []domain.TeamID) error {
	sources := refreshSources(env)
	authTest := desktopsession.AuthTest(env.SlackAPIURL)
	var refreshed, failed int
	for _, team := range targets {
		creds, err := auth.RefreshSession(ctx, team, sources, authTest)
		switch {
		case errors.Is(err, auth.ErrNotSignedIn):
			fmt.Fprintf(env.Stdout, refreshSignedOutHelp, team, team)
			failed++
		case err != nil:
			return err
		default:
			if err := ks.Set(ctx, team, creds); err != nil {
				return fmt.Errorf("store %s: %w", team, err)
			}
			fmt.Fprintf(env.Stdout, "Refreshed %s from its session source.\n", team)
			refreshed++
		}
	}
	if refreshed > 0 {
		reloadDaemon(ctx, env)
	}
	if refreshed == 0 && failed > 0 {
		return errNothingRefreshed
	}
	return nil
}

var errNothingRefreshed = errors.New("no workspace could be refreshed")

// refreshSources are the local desktop-session sources, or the ones a test
// injects.
func refreshSources(env Env) []auth.Source {
	if env.Sources != nil {
		return env.Sources()
	}
	return auth.LocalSources()
}

// reloadDaemon asks the daemon to read the keyring again. A daemon that is
// not running is only a note: the keyring is already updated and the
// change takes effect next time the daemon starts.
func reloadDaemon(ctx context.Context, env Env) {
	c, err := dialDaemon(ctx, env, refreshClient)
	if err != nil {
		fmt.Fprintln(env.Stdout, "The daemon is not running; it will use the refreshed session when it next starts.")
		return
	}
	defer c.Close()
	if _, err := c.Call(ctx, "workspaces.reload", nil); err != nil {
		fmt.Fprintf(env.Stdout, "Stored, but the daemon did not reload (%v); restart it to reconnect.\n", err)
		return
	}
	fmt.Fprintln(env.Stdout, "The daemon is reconnecting the refreshed workspaces.")
}

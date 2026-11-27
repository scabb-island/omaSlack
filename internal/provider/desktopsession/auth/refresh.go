package auth

import (
	"context"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

// Source is a place a desktop session can be read from: the Slack desktop
// app (Desktop) or a browser profile (Browser).
type Source interface {
	Workspaces(ctx context.Context) ([]Workspace, error)
}

// LocalSources are the desktop-session sources on this machine, in the same
// order auth add offers them: the Slack desktop app, then every browser
// profile. A source that is not installed is left out.
func LocalSources() []Source {
	var sources []Source
	if dir, err := FindDesktopDir(); err == nil {
		sources = append(sources, Desktop{Dir: dir, Passwords: SystemPasswords{}})
	}
	for _, b := range FindBrowsers() {
		sources = append(sources, b)
	}
	return sources
}

// RefreshSession re-reads team's session from sources and returns the
// credentials of the first source that holds a session for it which
// authTest accepts. It never prompts and never stores. ErrNotSignedIn
// means no source holds a valid session for team any more.
func RefreshSession(ctx context.Context, team domain.TeamID, sources []Source, authTest AuthTest) (keyring.DesktopSession, error) {
	for _, src := range sources {
		workspaces, err := src.Workspaces(ctx)
		if err != nil {
			continue
		}
		for _, w := range workspaces {
			if w.Team != team || w.NeedsSignIn() {
				continue
			}
			if _, err := authTest(ctx, w.Credentials); err == nil {
				return w.Credentials, nil
			}
		}
	}
	return keyring.DesktopSession{}, ErrNotSignedIn
}

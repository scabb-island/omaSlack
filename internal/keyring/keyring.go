// Package keyring stores each workspace's Slack credentials (spec
// slack-connection, "Credentials in the keyring"): in the Secret Service
// when one is available, otherwise in a 0600 file with a visible warning.
// Credentials never leave the daemon: no RPC method returns them.
package keyring

import (
	"context"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// ErrNotFound reports a workspace with no stored credentials.
var ErrNotFound = errors.New("no credentials stored for this workspace")

// Credentials is what a provider needs to talk to Slack for one workspace.
// It is sealed: DesktopSession and InternalApp are the only kinds, one per
// provider. Their String and GoString methods never print a secret.
type Credentials interface {
	// Provider is the name of the provider these credentials are for
	// ("desktop-session" or "internal-app").
	Provider() string
	fmt.Stringer
	fmt.GoStringer
	sealed()
}

// DesktopSession holds the desktop app or browser session: an xoxc token,
// its xoxd "d" cookie and the session's other cookies. Slack can reject
// the token with the d cookie alone and accept it with the whole set
// (GitLab issue #1), so the set is what gets sent; see CookieHeader.
type DesktopSession struct {
	Token string
	// Cookie is the d cookie's value, URL-encoded as Slack sends it.
	Cookie string
	// Cookies is every cookie of the session as a Cookie header
	// ("b=...; d=...; x=..."), d included. It is empty for entries stored
	// before omaslack kept the whole set.
	Cookies string
	// WorkspaceURL is the workspace's own address as auth.test gives it
	// ("https://acme.slack.com/"), or empty when unknown. Providers check
	// it before they send anything there.
	WorkspaceURL string
}

// CookieHeader is the Cookie header for this session: every cookie when
// they are known, otherwise the d cookie alone.
func (c DesktopSession) CookieHeader() string {
	if c.Cookies != "" {
		return c.Cookies
	}
	return "d=" + c.Cookie
}

// InternalApp holds an internal Slack app's xoxp user token and the xapp
// app-level token Socket Mode needs.
type InternalApp struct {
	UserToken string
	AppToken  string
	// WorkspaceURL is the workspace's own address as auth.test gives it,
	// or empty when unknown.
	WorkspaceURL string
}

func (DesktopSession) Provider() string { return "desktop-session" }
func (InternalApp) Provider() string    { return "internal-app" }

func (c DesktopSession) String() string {
	return fmt.Sprintf("desktop-session{token:%s cookie:%s cookies:%s}",
		presence(c.Token), presence(c.Cookie), presence(c.Cookies))
}

func (c InternalApp) String() string {
	return fmt.Sprintf("internal-app{userToken:%s appToken:%s}", presence(c.UserToken), presence(c.AppToken))
}

func (c DesktopSession) GoString() string { return c.String() }
func (c InternalApp) GoString() string    { return c.String() }

func (DesktopSession) sealed() {}
func (InternalApp) sealed()    {}

func presence(secret string) string {
	if secret == "" {
		return "missing"
	}
	return "set"
}

// Backend names where credentials are stored, for `omaslack doctor`.
type Backend string

const (
	SecretService Backend = "secret-service"
	File          Backend = "file" // 0600 file; doctor shows a warning
)

// Store keeps one set of credentials per workspace. Implementations must be
// safe for concurrent use.
type Store interface {
	// Get returns the credentials for team, or an error wrapping
	// ErrNotFound.
	Get(ctx context.Context, team domain.TeamID) (Credentials, error)

	// Set stores creds for team, replacing any previous ones. Idempotent.
	Set(ctx context.Context, team domain.TeamID, creds Credentials) error

	// Delete removes the credentials for team. Deleting what is not there
	// is not an error.
	Delete(ctx context.Context, team domain.TeamID) error

	// Teams lists every workspace with stored credentials.
	Teams(ctx context.Context) ([]domain.TeamID, error)

	// Backend says where this store keeps the credentials.
	Backend() Backend
}

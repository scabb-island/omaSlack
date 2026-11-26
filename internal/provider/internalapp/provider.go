// Package internalapp is the internal-app provider (spec slack-connection,
// "Interchangeable connection providers"): one workspace reached through an
// internal Slack app. The app's xoxp user token calls the Web API and its
// xapp app-level token opens Socket Mode for real-time events. Everything
// here uses public, documented Slack APIs.
package internalapp

import (
	"log/slog"
	"net/http"
	"net/url"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// webAPI is embedded under an unexported name, so Provider gets its
// History, Replies, Post and MarkRead without exposing the field.
type webAPI = slackwire.WebAPI

// Provider is the internal-app connection to one workspace. Create it with
// New; it is safe for concurrent use.
type Provider struct {
	*webAPI
	team   domain.TeamID
	creds  keyring.InternalApp
	apiURL string
	http   *http.Client
	log    *slog.Logger
}

// defaultAPIURL is Slack's Web API base. Method names are appended to it.
const defaultAPIURL = "https://slack.com/api/"

// Option configures a Provider.
type Option func(*Provider)

// WithAPIURL replaces Slack's Web API base URL (it must end in "/"). Tests
// point it at an httptest server.
func WithAPIURL(u string) Option { return func(p *Provider) { p.apiURL = u } }

// WithLogger sets where dropped events and messages are logged. The
// default is slog.Default().
func WithLogger(l *slog.Logger) Option { return func(p *Provider) { p.log = l } }

// New returns the provider for team, authenticated with creds.
func New(team domain.TeamID, creds keyring.InternalApp, opts ...Option) *Provider {
	p := &Provider{team: team, creds: creds, apiURL: defaultAPIURL, http: http.DefaultClient, log: slog.Default()}
	for _, opt := range opts {
		opt(p)
	}
	p.webAPI = slackwire.NewWebAPI(p.apiURL, p.http, bearer(creds.UserToken), p.log.With("provider", "internal-app", "team", team))
	return p
}

// bearer signs Web API requests with the user token, the documented way.
func bearer(token string) slackwire.Signer {
	return func(_ url.Values, h http.Header) { h.Set("Authorization", "Bearer "+token) }
}

// Team is the workspace this provider is bound to.
func (p *Provider) Team() domain.TeamID { return p.team }

// capabilities is what a user token plus Socket Mode can do through public
// APIs: Socket Mode delivers events (realtime); search.messages, the files.*
// methods, users.setPresence and emoji.list all accept a user token (search,
// files, presenceWrite, customEmoji).
//
// Left out on purpose: typing, because the Events API has no typing event
// (user_typing exists only in the deprecated RTM API, which internal apps
// cannot open), and sections, because Slack's sidebar sections have no
// public API.
var capabilities = domain.NewCapabilities(
	domain.Realtime, domain.Search, domain.Files, domain.PresenceWrite, domain.CustomEmoji,
)

// Capabilities is what this provider supports.
func (p *Provider) Capabilities() domain.Capabilities { return capabilities }

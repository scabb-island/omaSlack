// Adapted from github.com/gammons/slk internal/slack/client.go (MIT, Copyright (c) 2026 Grant Ammons).

// Package desktopsession is the desktop-session provider (spec
// slack-connection, "Interchangeable connection providers"): one
// workspace reached the way Slack's own web client reaches it, with the
// xoxc token and the d cookie of a signed-in session. It calls the Web API
// with those credentials and opens the web client's WebSocket for
// real-time events. That protocol is unofficial, which is why the
// workspace is only registered after the user's explicit consent.
package desktopsession

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

// webAPI is embedded under an unexported name, so Provider gets its
// History, Replies, Post and MarkRead without exposing the field.
type webAPI = slackwire.WebAPI

// Provider is the desktop-session connection to one workspace. Create it
// with New; it is safe for concurrent use.
type Provider struct {
	*webAPI
	team      domain.TeamID
	creds     keyring.DesktopSession
	apiURL    string
	socketURL string
	// readTimeout is how long the socket may stay silent; tests shorten it.
	readTimeout time.Duration
	http        *http.Client
	log         *slog.Logger
}

const (
	// defaultAPIURL is Slack's Web API base. Method names are appended.
	defaultAPIURL = "https://slack.com/api/"
	// defaultSocketURL is the web client's real-time endpoint. The query
	// string is added by Connect.
	defaultSocketURL = "wss://wss-primary.slack.com/"
)

// userAgent is the Chrome on Linux the web client runs in. Slack's
// anomaly detection (Enterprise Grid in particular) flags requests that do
// not look like they came from a browser.
const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"

// Option configures a Provider.
type Option func(*Provider)

// WithAPIURL replaces Slack's Web API base URL (it must end in "/"). Tests
// point it at an httptest server.
func WithAPIURL(u string) Option { return func(p *Provider) { p.apiURL = u } }

// WithSocketURL replaces the web client's WebSocket URL (it must end in
// "/"). Tests point it at an httptest server.
func WithSocketURL(u string) Option { return func(p *Provider) { p.socketURL = u } }

// WithLogger sets where dropped events and messages are logged. The
// default is slog.Default().
func WithLogger(l *slog.Logger) Option { return func(p *Provider) { p.log = l } }

// New returns the provider for team, authenticated with creds.
func New(team domain.TeamID, creds keyring.DesktopSession, opts ...Option) *Provider {
	p := &Provider{
		team: team, creds: creds,
		apiURL: defaultAPIURL, socketURL: defaultSocketURL, readTimeout: defaultReadTimeout,
		http: http.DefaultClient, log: slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}
	p.webAPI = slackwire.NewWebAPI(p.apiURL, p.http, p.sign, p.log.With("provider", "desktop-session", "team", team))
	return p
}

// sign authenticates a Web API request the way the web client does: the
// xoxc token goes in the form, never in an Authorization header, and the
// d cookie goes with it. Slack rejects an xoxc token without its cookie.
func (p *Provider) sign(form url.Values, h http.Header) {
	form.Set("token", p.creds.Token)
	p.browserHeaders(h)
}

// browserHeaders are what the web client's requests carry: the d cookie,
// and the Origin and User-Agent of a Chrome tab on app.slack.com.
func (p *Provider) browserHeaders(h http.Header) {
	h.Set("Cookie", "d="+p.creds.Cookie)
	h.Set("Origin", "https://app.slack.com")
	h.Set("User-Agent", userAgent)
}

// Team is the workspace this provider is bound to.
func (p *Provider) Team() domain.TeamID { return p.team }

// capabilities is everything the web client itself can do, because this
// provider speaks its protocol with its credentials: the socket delivers
// events, typing indicators (user_typing) and the read markers moved in
// other clients (realtime, typing); search.messages, the files.* methods,
// users.setPresence and emoji.list accept an xoxc token (search, files,
// presenceWrite, customEmoji); and the sidebar sections the web client
// shows come from its own users.channelSections.list (sections).
//
// Declaring a capability promises that the protocol allows it, not that
// this package already calls it: M1 implements the provider.Provider
// methods, and the operations behind the other capabilities land with the
// features that use them.
var capabilities = domain.NewCapabilities(
	domain.Realtime, domain.Search, domain.Files, domain.PresenceWrite,
	domain.Typing, domain.CustomEmoji, domain.Sections,
)

// Capabilities is what this provider supports.
func (p *Provider) Capabilities() domain.Capabilities { return capabilities }

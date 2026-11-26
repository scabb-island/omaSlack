// Adapted from github.com/gammons/slk cmd/slk/onboarding_browser.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

// The browser login signs a workspace in without the desktop app. It needs
// the same two values: the xoxc token and the d cookie. The d cookie is
// HttpOnly, so no DevTools console snippet can read it, but "Copy as cURL"
// of any request to a Slack /api/ endpoint carries both. The bash, cmd and
// PowerShell forms all work, as does a bare token followed by the cookie
// on a line of its own.
var (
	xoxcPattern = regexp.MustCompile(`xoxc-[A-Za-z0-9-]+`)
	// The d cookie as "d=<value>" (bash and cmd forms, a Cookie header),
	// not d-s, xd or any other cookie whose name ends in d.
	dCookiePattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])d=(xoxd-[^;\s"'\\]+)`)
	// The PowerShell form:
	// New-Object System.Net.Cookie("d", "xoxd-...", "/", ".slack.com").
	psCookiePattern = regexp.MustCompile(`Cookie\(\s*"d"\s*,\s*"(xoxd-[^"]+)"`)
	// A value on a line of its own, without the "d=" prefix.
	bareCookiePattern = regexp.MustCompile(`(?m)^[ \t]*(xoxd-[^;\s"'\\]+);?[ \t]*\r?$`)
)

// ParseCurl extracts a browser session from a pasted "Copy as cURL"
// command, or from a token and a cookie pasted one per line. It returns
// ErrNoToken or ErrNoCookie for what it cannot find; a caller that asked
// for the cookie separately can join the two pastes with a newline.
func ParseCurl(paste string) (keyring.DesktopSession, error) {
	// The cmd form escapes with carets (^", ^%, %^2F); none belongs to a
	// token or a cookie.
	if strings.Contains(paste, `^"`) {
		paste = strings.ReplaceAll(paste, "^", "")
	}
	token := xoxcPattern.FindString(paste)
	if token == "" {
		return keyring.DesktopSession{}, ErrNoToken
	}
	cookie := findCookie(paste)
	if cookie == "" {
		return keyring.DesktopSession{}, ErrNoCookie
	}
	return keyring.DesktopSession{Token: token, Cookie: encodeCookie(cookie)}, nil
}

func findCookie(paste string) string {
	for _, p := range []*regexp.Regexp{dCookiePattern, psCookiePattern, bareCookiePattern} {
		if m := p.FindStringSubmatch(paste); m != nil {
			return m[1]
		}
	}
	return ""
}

// encodeCookie returns the d cookie URL-encoded, the form the desktop app
// stores and Slack sends. DevTools' Application tab can show it decoded
// (xoxd-AbC/def+Ghi==); an encoded value has none of /+= and is kept.
func encodeCookie(cookie string) string {
	if !strings.ContainsAny(cookie, "/+=") {
		return cookie
	}
	return url.QueryEscape(cookie)
}

// Identity is what Slack's auth.test answers about a session, as received.
type Identity struct {
	TeamID string
	Team   string
	URL    string
}

// AuthTest calls Slack's auth.test with a session. The provider supplies
// it; this package never talks to Slack.
type AuthTest func(ctx context.Context, creds keyring.DesktopSession) (Identity, error)

// FromCurl parses a pasted cURL command and asks Slack, through authTest,
// which workspace the session belongs to. It checks the session too: an
// expired one fails in authTest.
func FromCurl(ctx context.Context, paste string, authTest AuthTest) (Workspace, error) {
	creds, err := ParseCurl(paste)
	if err != nil {
		return Workspace{}, err
	}
	id, err := authTest(ctx, creds)
	if err != nil {
		return Workspace{}, err
	}
	team, err := domain.ParseTeamID(id.TeamID)
	if err != nil {
		return Workspace{}, fmt.Errorf("auth.test: %w", err)
	}
	return Workspace{Team: team, Name: id.Team, URL: id.URL, Credentials: creds}, nil
}

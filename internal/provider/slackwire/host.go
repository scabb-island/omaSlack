package slackwire

import (
	"net/url"
	"regexp"
	"strings"
)

// DefaultAPIURL is Slack's Web API base. Method names are appended to it.
const DefaultAPIURL = "https://slack.com/api/"

// workspaceHost is a workspace's own domain: one or more DNS labels under
// slack.com ("acme.slack.com", "acme.enterprise.slack.com").
var workspaceHost = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+slack\.com$`)

// APIBase is where a provider sends its Web API calls. Slack's web client
// calls the Web API on the workspace's own domain, the url auth.test
// returns, so when base is Slack's default and workspaceURL is a Slack
// workspace address over HTTPS, the calls go to that workspace's /api/.
// Any other workspaceURL is ignored, so a stored value can never send
// credentials outside Slack. Any other base (a test server) is kept.
func APIBase(base, workspaceURL string) string {
	if base != DefaultAPIURL {
		return base
	}
	if host, ok := parseWorkspaceURL(workspaceURL); ok {
		return "https://" + host + "/api/"
	}
	return base
}

// parseWorkspaceURL returns the host of a workspace address: https, no
// user, port, path, query or fragment, and a host under slack.com.
func parseWorkspaceURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", false
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	return host, workspaceHost.MatchString(host)
}

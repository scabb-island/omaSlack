package slackwire

import "testing"

// A stored workspace URL decides where credentials go, so only a Slack
// workspace address over HTTPS is ever used; anything else falls back to
// slack.com.
func TestAPIBaseUsesOnlyASlackWorkspaceAddress(t *testing.T) {
	for _, tc := range []struct{ workspace, want string }{
		{"https://acme.slack.com/", "https://acme.slack.com/api/"},
		{"https://acme.slack.com", "https://acme.slack.com/api/"},
		{"https://ACME.Slack.com/", "https://acme.slack.com/api/"},
		{"https://acme.enterprise.slack.com/", "https://acme.enterprise.slack.com/api/"},
		{"", DefaultAPIURL},
		{"http://acme.slack.com/", DefaultAPIURL},
		{"https://slack.com/", DefaultAPIURL},
		{"https://.slack.com/", DefaultAPIURL},
		{"https://acme.slack.com.evil.example/", DefaultAPIURL},
		{"https://evilslack.com/", DefaultAPIURL},
		{"https://acme.slack.com:8443/", DefaultAPIURL},
		{"https://user@acme.slack.com/", DefaultAPIURL},
		{"https://acme.slack.com/api/", DefaultAPIURL},
		{"https://acme.slack.com/?x=1", DefaultAPIURL},
		{"https://acme.slack.com/#x", DefaultAPIURL},
		{"https://ac_me.slack.com/", DefaultAPIURL},
		{"https://acme..slack.com/", DefaultAPIURL},
		{"not a url", DefaultAPIURL},
	} {
		if got := APIBase(DefaultAPIURL, tc.workspace); got != tc.want {
			t.Errorf("APIBase(default, %q) = %q, want %q", tc.workspace, got, tc.want)
		}
	}
}

// A base other than Slack's default (a test server) is kept as it is.
func TestAPIBaseKeepsAnExplicitBase(t *testing.T) {
	if got := APIBase("http://127.0.0.1:1234/api/", "https://acme.slack.com/"); got != "http://127.0.0.1:1234/api/" {
		t.Errorf("APIBase = %q, want the explicit base", got)
	}
}

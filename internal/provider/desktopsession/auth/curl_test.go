// Adapted from github.com/gammons/slk cmd/slk/onboarding_browser_test.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"context"
	"errors"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

// Shapes of "Copy as cURL" as Chrome and Firefox produce them, trimmed to
// what matters. The values are fake. The cmd and PowerShell ones follow
// Chrome's escaping rules (^" around strings, ^% and %^ around a percent;
// the cookies in a WebRequestSession).
const (
	chromeCurl = `curl 'https://acme.slack.com/api/conversations.history?_x_id=abc&slack_route=T0123' \
  -H 'accept: */*' \
  -b 'b=abc123; d=xoxd-AbC%2Fdef%2BGhi%3D%3D; d-s=1700000000; lc=1700000000' \
  -H 'origin: https://app.slack.com' \
  --data-raw $'------WebKitFormBoundaryXYZ\r\nContent-Disposition: form-data; name="token"\r\n\r\nxoxc-1111-2222-3333-abcdef\r\n------WebKitFormBoundaryXYZ--\r\n'
`
	firefoxCurl = `curl 'https://acme.slack.com/api/client.counts' -X POST -H 'Content-Type: application/x-www-form-urlencoded' -H 'Cookie: d-s=17; d=xoxd-ZZZ%2F%3D; b=x' --data-raw 'token=xoxc-9999-8888-aa&_x_reason=x'
`
	cmdCurl = `curl ^"https://acme.slack.com/api/conversations.history?_x_id=abc^&slack_route=T0123^" ^
  -H ^"accept: */*^" ^
  -b ^"b=abc123; d=xoxd-AbC^%^2Fdef^%^2BGhi^%^3D^%^3D; d-s=1700000000^" ^
  --data-raw ^"token=xoxc-1111-2222-3333-abcdef^&_x_reason=x^"
`
	powershellCurl = "$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession\n" +
		"$session.Cookies.Add((New-Object System.Net.Cookie(\"b\", \"abc123\", \"/\", \".slack.com\")))\n" +
		"$session.Cookies.Add((New-Object System.Net.Cookie(\"d\", \"xoxd-AbC%2Fdef\", \"/\", \".slack.com\")))\n" +
		"Invoke-WebRequest -UseBasicParsing -Uri \"https://acme.slack.com/api/client.counts\" `\n" +
		"-WebSession $session `\n" +
		"-Body \"token=xoxc-9999-8888-aa&_x_reason=x\"\n"
	powershellHeadersCurl = "Invoke-WebRequest -UseBasicParsing -Uri \"https://acme.slack.com/api/client.counts\" `\n" +
		"-Headers @{\n" +
		"\"Cookie\" = \"b=x; d=xoxd-ZZZ%2F%3D; d-s=17\"\n" +
		"} `\n" +
		"-Body \"token=xoxc-9999-8888-aa&_x_reason=x\"\n"
)

func TestParseCurl(t *testing.T) {
	for _, tt := range []struct {
		name, paste, token, cookie string
	}{
		{"chrome", chromeCurl, "xoxc-1111-2222-3333-abcdef", "xoxd-AbC%2Fdef%2BGhi%3D%3D"},
		{"firefox, d-s before d", firefoxCurl, "xoxc-9999-8888-aa", "xoxd-ZZZ%2F%3D"},
		{"chrome cmd", cmdCurl, "xoxc-1111-2222-3333-abcdef", "xoxd-AbC%2Fdef%2BGhi%3D%3D"},
		{"chrome powershell", powershellCurl, "xoxc-9999-8888-aa", "xoxd-AbC%2Fdef"},
		{"powershell, cookie in the headers", powershellHeadersCurl, "xoxc-9999-8888-aa", "xoxd-ZZZ%2F%3D"},
		{"a cookie named xd before d", `-b 'xd=xoxd-WRONG; d=xoxd-RIGHT' token=xoxc-1`, "xoxc-1", "xoxd-RIGHT"},
		{"a cookie named x-d before d", `-b 'x-d=xoxd-WRONG; d=xoxd-RIGHT' token=xoxc-1`, "xoxc-1", "xoxd-RIGHT"},
		{"token, then the cookie decoded on its own line", "xoxc-1-2\nxoxd-AbC/def+Ghi==\n", "xoxc-1-2", "xoxd-AbC%2Fdef%2BGhi%3D%3D"},
		{"token, then the cookie, CRLF", "xoxc-1-2\r\n  xoxd-Q%2F\r\n", "xoxc-1-2", "xoxd-Q%2F"},
		{"bare cookie with its trailing semicolon", "xoxc-1\nxoxd-Q%2F;\n", "xoxc-1", "xoxd-Q%2F"},
		{"cookie first in the header", `-H 'cookie: d=xoxd-A1; b=2' token=xoxc-1-2`, "xoxc-1-2", "xoxd-A1"},
		{"cookie with its name", "xoxc-1\nd=xoxd-Q\n", "xoxc-1", "xoxd-Q"},
	} {
		got, err := ParseCurl(tt.paste)
		want := keyring.DesktopSession{Token: tt.token, Cookie: tt.cookie}
		if err != nil || got != want {
			t.Errorf("%s: ParseCurl = %#v, %v; want %#v", tt.name, got, err, want)
			if got.Token != tt.token || got.Cookie != tt.cookie {
				t.Errorf("%s: token %t, cookie %t", tt.name, got.Token == tt.token, got.Cookie == tt.cookie)
			}
		}
	}
}

func TestParseCurlNamesWhatIsMissing(t *testing.T) {
	for _, tt := range []struct {
		name, paste string
		want        error
	}{
		{"empty", "", ErrNoToken},
		{"the page, not an api call", `curl 'https://app.slack.com/client/T1' -b 'd=xoxd-A'`, ErrNoToken},
		{"bare token", "xoxc-1-2-3\n", ErrNoCookie},
		{"only d-s", `-b 'd-s=xoxd-nope' token=xoxc-1`, ErrNoCookie},
		{"xoxd inside other text", "xoxc-1\nsee xoxd-Q in the docs\n", ErrNoCookie},
		{"xoxd followed by other text", "xoxc-1\nxoxd-Q is my cookie\n", ErrNoCookie},
	} {
		if _, err := ParseCurl(tt.paste); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestFromCurlAsksSlackWhoTheSessionIs(t *testing.T) {
	var asked keyring.DesktopSession
	authTest := func(_ context.Context, creds keyring.DesktopSession) (Identity, error) {
		asked = creds
		return Identity{TeamID: "T0123", Team: "Acme", URL: "https://acme.slack.com/"}, nil
	}
	got, err := FromCurl(context.Background(), firefoxCurl, authTest)
	if err != nil {
		t.Fatalf("FromCurl: %v", err)
	}
	creds := keyring.DesktopSession{Token: "xoxc-9999-8888-aa", Cookie: "xoxd-ZZZ%2F%3D"}
	team, _ := domain.ParseTeamID("T0123")
	want := Workspace{Team: team, Name: "Acme", URL: "https://acme.slack.com/", Credentials: creds}
	if got != want || asked != creds {
		t.Errorf("FromCurl = %v after asking with %v; want %v", got, asked, want)
	}
}

func TestFromCurlFailures(t *testing.T) {
	boom := errors.New("invalid_auth")
	answer := func(id Identity, err error) AuthTest {
		return func(context.Context, keyring.DesktopSession) (Identity, error) { return id, err }
	}
	for _, tt := range []struct {
		name     string
		paste    string
		authTest AuthTest
		want     error
	}{
		{"nothing pasted", "", answer(Identity{}, nil), ErrNoToken},
		{"Slack refuses", firefoxCurl, answer(Identity{}, boom), boom},
		{"Slack answers a bad team", firefoxCurl, answer(Identity{TeamID: "nope"}, nil), domain.ErrInvalidID},
	} {
		if _, err := FromCurl(context.Background(), tt.paste, tt.authTest); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}
}

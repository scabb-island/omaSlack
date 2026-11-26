package desktopsession

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/slackwire"
)

func TestWebAPICallsCarryTheTokenInTheFormAndTheCookie(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/conversations.history": jsonBody(http.StatusOK, `{"ok":true,"messages":[{"user":"U0002","text":"hi","ts":"1700000100.000100"}]}`),
	})
	p, _ := f.provider(t)

	got, err := p.History(context.Background(), parse(t, domain.ParseConversationID, "C0001"), domain.TS{}, 10)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.Message{{
		TS:     parse(t, domain.ParseTS, "1700000100.000100"),
		Author: parse(t, domain.ParseAuthor, "U0002"),
		Text:   "hi",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("History() = %+v, want %+v", got, want)
	}
	req := f.requests()[0]
	if req.Form["token"][0] != testToken || req.Form["channel"][0] != "C0001" {
		t.Errorf("form = %v, want the xoxc token and the channel", req.Form)
	}
	if req.Cookies != testCookies {
		t.Errorf("cookies = %q, want every cookie of the session", req.Cookies)
	}
	if auth := req.Header.Get("Authorization"); auth != "" {
		t.Errorf("Authorization = %q; the web client sends none", auth)
	}
	assertBrowserHeaders(t, req.Header)
}

// assertBrowserHeaders checks the headers that make a request look like
// it came from the web client.
func assertBrowserHeaders(t *testing.T, h http.Header) {
	t.Helper()
	if got := h.Get("Origin"); got != "https://app.slack.com" {
		t.Errorf("Origin = %q, want https://app.slack.com", got)
	}
	if got := h.Get("User-Agent"); got != userAgent {
		t.Errorf("User-Agent = %q, want %q", got, userAgent)
	}
}

func TestRejectedCredentialsAndRateLimitsAreTyped(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"/api/conversations.mark": jsonBody(http.StatusOK, `{"ok":false,"error":"invalid_auth"}`),
		"/api/chat.postMessage": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "7")
			jsonBody(http.StatusTooManyRequests, `{}`)(w, r)
		},
	})
	p, _ := f.provider(t)
	conv := parse(t, domain.ParseConversationID, "C0001")

	err := p.MarkRead(context.Background(), conv, parse(t, domain.ParseTS, "1700000100.000100"))
	if !errors.Is(err, provider.ErrAuthExpired) {
		t.Errorf("MarkRead() err = %v, want ErrAuthExpired", err)
	}
	_, err = p.Post(context.Background(), conv, domain.TS{}, "hi")
	var limited *provider.RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 7*time.Second {
		t.Errorf("Post() err = %v, want a 7s RateLimitedError", err)
	}
}

// An entry stored before the whole cookie set was kept sends the d cookie
// alone, as before.
func TestAnEntryWithoutTheCookieSetSendsTheDCookieAlone(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{"/api/auth.test": jsonBody(http.StatusOK, authOK)})
	p := New(parse(t, domain.ParseTeamID, "T0001"), keyring.DesktopSession{Token: testToken, Cookie: testCookie},
		WithAPIURL(f.srv.URL+"/api/"))
	if _, err := AuthTest(f.srv.URL+"/api/")(context.Background(), keyring.DesktopSession{Token: testToken, Cookie: testCookie}); err != nil {
		t.Fatal(err)
	}
	if err := p.Call(context.Background(), "auth.test", url.Values{}, &slackwire.Envelope{}); err != nil {
		t.Fatal(err)
	}
	for _, req := range f.requests() {
		if req.Cookies != "d="+testCookie {
			t.Errorf("%s: cookies = %q, want the d cookie alone", req.Path, req.Cookies)
		}
	}
}

// recorder answers every request with ok and keeps the URLs it was sent.
type recorder struct{ urls []string }

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL.String())
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(authOK)), Request: req,
	}, nil
}

// The web client calls the Web API on the workspace's own domain; a
// stored URL that is not a Slack workspace's never receives anything.
func TestCallsGoToTheWorkspacesOwnDomain(t *testing.T) {
	for workspace, want := range map[string]string{
		"https://acme.slack.com/":   "https://acme.slack.com/api/auth.test",
		"https://acme.example.com/": "https://slack.com/api/auth.test",
		"http://acme.slack.com/":    "https://slack.com/api/auth.test",
		"":                          "https://slack.com/api/auth.test",
	} {
		rec := &recorder{}
		creds := testCreds()
		creds.WorkspaceURL = workspace
		p := New(parse(t, domain.ParseTeamID, "T0001"), creds, WithHTTPClient(&http.Client{Transport: rec}))
		if err := p.Call(context.Background(), "auth.test", url.Values{}, &slackwire.Envelope{}); err != nil {
			t.Fatalf("%q: %v", workspace, err)
		}
		if len(rec.urls) != 1 || rec.urls[0] != want {
			t.Errorf("workspace %q: requests = %v, want %s", workspace, rec.urls, want)
		}
	}
}

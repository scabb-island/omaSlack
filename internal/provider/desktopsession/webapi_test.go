package desktopsession

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
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
	if req.Cookie != testCookie {
		t.Errorf("d cookie = %q, want %q", req.Cookie, testCookie)
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

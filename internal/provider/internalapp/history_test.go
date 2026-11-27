package internalapp

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

func TestHistoryReturnsTheParsedMessagesNewestFirst(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.history": okJSON(readFixture(t, "conversations.history.json")),
	})
	p, _ := newTestProvider(t, f)

	got, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.Message{
		{
			TS:       mustTS(t, "1700000300.000300"),
			ThreadTS: mustTS(t, "1700000100.000100"),
			Author:   mustAuthor(t, "U0002"),
			Text:     "edited reply",
			Edited:   mustTS(t, "1700000400.000000"),
		},
		{
			TS:     mustTS(t, "1700000200.000200"),
			Author: mustAuthor(t, "B0001"),
			Text:   "deploy finished",
		},
		{
			TS:          mustTS(t, "1700000100.000100"),
			ThreadTS:    mustTS(t, "1700000100.000100"),
			Author:      mustAuthor(t, "U0001"),
			Text:        "hello",
			ReplyCount:  1,
			LatestReply: mustTS(t, "1700000300.000300"),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("History() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestHistoryLogsTheMessagesItCannotRepresent(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.history": okJSON(readFixture(t, "conversations.history.json")),
	})
	p, logs := newTestProvider(t, f)

	if _, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "1700000050.000050") {
		t.Fatalf("the dropped message without an author is not logged; logs:\n%s", logs)
	}
}

func TestHistoryAsksForTheLatestPageWhenBeforeIsZero(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.history": okJSON([]byte(`{"ok":true,"messages":[]}`)),
	})
	p, _ := newTestProvider(t, f)

	if _, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50); err != nil {
		t.Fatal(err)
	}

	c := f.recorded()[0]
	if c.Auth != "Bearer "+testUserToken {
		t.Errorf("Authorization = %q, want the user token", c.Auth)
	}
	if c.Form.Get("channel") != "C0001" || c.Form.Get("limit") != "50" {
		t.Errorf("form = %v, want channel C0001 and limit 50", c.Form)
	}
	if c.Form.Has("latest") {
		t.Errorf("form = %v, want no latest", c.Form)
	}
}

func TestHistoryAsksForMessagesOlderThanBefore(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.history": okJSON([]byte(`{"ok":true,"messages":[]}`)),
	})
	p, _ := newTestProvider(t, f)

	if _, err := p.History(context.Background(), mustConv(t, "C0001"), mustTS(t, "1700000100.000100"), 20); err != nil {
		t.Fatal(err)
	}

	c := f.recorded()[0]
	if c.Form.Get("latest") != "1700000100.000100" || c.Form.Get("limit") != "20" {
		t.Errorf("form = %v, want latest 1700000100.000100 and limit 20", c.Form)
	}
	if c.Form.Has("inclusive") {
		t.Errorf("form = %v, want before itself excluded", c.Form)
	}
}

func TestSlackAuthErrorsWrapErrAuthExpired(t *testing.T) {
	for _, code := range []string{"invalid_auth", "token_revoked", "account_inactive", "not_authed"} {
		t.Run(code, func(t *testing.T) {
			f := newFakeSlack(t, map[string]http.HandlerFunc{
				"conversations.history": okJSON([]byte(`{"ok":false,"error":"` + code + `"}`)),
			})
			p, _ := newTestProvider(t, f)

			_, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50)
			if !errors.Is(err, provider.ErrAuthExpired) {
				t.Fatalf("err = %v, want ErrAuthExpired", err)
			}
			if !strings.Contains(err.Error(), code) {
				t.Errorf("err = %q, want it to name %s", err, code)
			}
		})
	}
}

func TestOtherSlackErrorsNameTheMethodAndCode(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.history": okJSON([]byte(`{"ok":false,"error":"channel_not_found"}`)),
	})
	p, _ := newTestProvider(t, f)

	_, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50)
	if err == nil || errors.Is(err, provider.ErrAuthExpired) {
		t.Fatalf("err = %v, want a non-auth error", err)
	}
	if err.Error() != "conversations.history: channel_not_found" {
		t.Errorf("err = %q", err)
	}
}

func TestHTTP429IsRateLimitedWithRetryAfter(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.history": respond(http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}, nil),
	})
	p, _ := newTestProvider(t, f)

	_, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50)
	var rl *provider.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want *RateLimitedError", err)
	}
	if rl.Method != "conversations.history" || rl.RetryAfter != 30*time.Second {
		t.Errorf("err = %+v, want conversations.history after 30s", rl)
	}
}

func TestHTTP429WithoutAUsableRetryAfterWaitsOneMinute(t *testing.T) {
	for _, header := range []map[string]string{nil, {"Retry-After": "soon"}} {
		f := newFakeSlack(t, map[string]http.HandlerFunc{
			"conversations.history": respond(http.StatusTooManyRequests, header, nil),
		})
		p, _ := newTestProvider(t, f)

		_, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50)
		var rl *provider.RateLimitedError
		if !errors.As(err, &rl) || rl.RetryAfter != time.Minute {
			t.Errorf("header %v: err = %v, want a one-minute RateLimitedError", header, err)
		}
	}
}

func TestOtherHTTPStatusesAreErrors(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.history": respond(http.StatusBadGateway, nil, []byte("<html>bad gateway</html>")),
	})
	p, _ := newTestProvider(t, f)

	_, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want one naming HTTP 502", err)
	}
}

func TestUndecodableResponsesAreErrors(t *testing.T) {
	f := newFakeSlack(t, map[string]http.HandlerFunc{
		"conversations.history": okJSON([]byte(`not json`)),
	})
	p, _ := newTestProvider(t, f)

	if _, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50); err == nil {
		t.Fatal("err = nil, want a decode error")
	}
}

func TestUnreachableSlackIsAnError(t *testing.T) {
	f := newFakeSlack(t, nil)
	p, _ := newTestProvider(t, f)
	f.srv.Close()

	if _, err := p.History(context.Background(), mustConv(t, "C0001"), domain.TS{}, 50); err == nil {
		t.Fatal("err = nil, want a transport error")
	}
}

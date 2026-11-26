package slackwire

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
)

// slack is a fake Web API. Each method answers with the body of the page
// the request's cursor names ("" for the first), and every form is kept.
type slack struct {
	t      *testing.T
	mu     sync.Mutex
	forms  []url.Values
	header http.Header
}

// serve starts a server whose methods answer status and body, by method
// and cursor, and returns a WebAPI for it that logs into logs.
func (s *slack) serve(pages map[string]map[string]string, status int, header map[string]string) (*WebAPI, *bytes.Buffer) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			s.t.Errorf("parse form: %v", err)
		}
		s.mu.Lock()
		s.forms = append(s.forms, r.PostForm)
		s.header = r.Header.Clone()
		s.mu.Unlock()
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(pages[strings.TrimPrefix(r.URL.Path, "/api/")][r.PostForm.Get("cursor")]))
	}))
	s.t.Cleanup(srv.Close)
	var logs bytes.Buffer
	sign := func(form url.Values, h http.Header) { form.Set("token", "secret"); h.Set("X-Signed", "yes") }
	return NewWebAPI(srv.URL+"/api/", srv.Client(), sign, slog.New(slog.NewTextHandler(&logs, nil))), &logs
}

func fakeSlack(t *testing.T, pages map[string]map[string]string) (*WebAPI, *slack, *bytes.Buffer) {
	s := &slack{t: t}
	api, logs := s.serve(pages, http.StatusOK, nil)
	return api, s, logs
}

func (s *slack) form(i int) url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.forms[i]
}

func TestHistoryAsksForAPageAndDropsWhatDoesNotParse(t *testing.T) {
	api, s, logs := fakeSlack(t, map[string]map[string]string{"conversations.history": {"": `{"ok":true,"messages":[
		{"user":"U0002","text":"newest","ts":"1700000200.000200"},
		{"text":"no author","ts":"1700000150.000150"},
		{"user":"U0001","text":"oldest","ts":"1700000100.000100"}]}`}})
	before := must(t, domain.ParseTS, "1700000300.000300")

	got, err := api.History(context.Background(), must(t, domain.ParseConversationID, "C0001"), before, 3)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Text != "newest" || got[1].Text != "oldest" {
		t.Fatalf("History() = %+v, want newest and oldest", got)
	}
	if !strings.Contains(logs.String(), "1700000150.000150") {
		t.Errorf("the dropped message is not logged; logs:\n%s", logs)
	}
	want := url.Values{"channel": {"C0001"}, "limit": {"3"}, "latest": {"1700000300.000300"}, "token": {"secret"}}
	if f := s.form(0); !reflect.DeepEqual(f, want) {
		t.Errorf("form = %v, want %v", f, want)
	}
	if s.header.Get("X-Signed") != "yes" || s.header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("headers = %v, want the signer's and a form content type", s.header)
	}
}

func TestHistoryFromTheLatestSendsNoLatest(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"conversations.history": {"": `{"ok":true,"messages":[]}`}})

	if _, err := api.History(context.Background(), must(t, domain.ParseConversationID, "C0001"), domain.TS{}, 1); err != nil {
		t.Fatal(err)
	}
	if s.form(0).Has("latest") {
		t.Errorf("form = %v, want no latest", s.form(0))
	}
}

func TestRepliesFollowsEveryPageAndLeavesOutTheRoot(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"conversations.replies": {
		"": `{"ok":true,"messages":[{"user":"U0001","ts":"1700000100.000100","thread_ts":"1700000100.000100"},
			{"user":"U0002","text":"one","ts":"1700000200.000200","thread_ts":"1700000100.000100"}],
			"response_metadata":{"next_cursor":"page2"}}`,
		"page2": `{"ok":true,"messages":[{"user":"U0001","ts":"1700000100.000100","thread_ts":"1700000100.000100"},
			{"user":"U0002","text":"two","ts":"1700000300.000300","thread_ts":"1700000100.000100"}]}`,
	}})
	root := must(t, domain.ParseTS, "1700000100.000100")

	got, err := api.Replies(context.Background(), must(t, domain.ParseConversationID, "C0001"), root)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Text != "one" || got[1].Text != "two" {
		t.Fatalf("Replies() = %+v, want one and two", got)
	}
	first := url.Values{"channel": {"C0001"}, "ts": {"1700000100.000100"}, "limit": {"200"}, "token": {"secret"}}
	if f := s.form(0); !reflect.DeepEqual(f, first) {
		t.Errorf("first form = %v, want %v", f, first)
	}
	if c := s.form(1).Get("cursor"); c != "page2" {
		t.Errorf("second cursor = %q, want page2", c)
	}
}

func TestRepliesFailsWhenAPageFails(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"conversations.replies": {
		"":      `{"ok":true,"messages":[],"response_metadata":{"next_cursor":"page2"}}`,
		"page2": `{"ok":false,"error":"thread_not_found"}`,
	}})

	got, err := api.Replies(context.Background(), must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100"))
	if err == nil || got != nil {
		t.Fatalf("Replies() = %v, %v; want nil and an error", got, err)
	}
}

func TestPostReturnsTheStoredMessage(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"chat.postMessage": {
		"": `{"ok":true,"message":{"user":"U0001","text":"hi","ts":"1700000500.000500","thread_ts":"1700000100.000100"}}`,
	}})
	thread := must(t, domain.ParseTS, "1700000100.000100")

	got, err := api.Post(context.Background(), must(t, domain.ParseConversationID, "C0001"), thread, "hi")
	if err != nil {
		t.Fatal(err)
	}

	if got.TS != must(t, domain.ParseTS, "1700000500.000500") || got.ThreadTS != thread || got.Text != "hi" {
		t.Errorf("Post() = %+v", got)
	}
	want := url.Values{"channel": {"C0001"}, "text": {"hi"}, "thread_ts": {"1700000100.000100"}, "token": {"secret"}}
	if f := s.form(0); !reflect.DeepEqual(f, want) {
		t.Errorf("form = %v, want %v", f, want)
	}
}

func TestPostOutsideAThreadSendsNoThread(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"chat.postMessage": {
		"": `{"ok":true,"message":{"user":"U0001","ts":"1700000500.000500"}}`,
	}})

	if _, err := api.Post(context.Background(), must(t, domain.ParseConversationID, "C0001"), domain.TS{}, "hi"); err != nil {
		t.Fatal(err)
	}
	if s.form(0).Has("thread_ts") {
		t.Errorf("form = %v, want no thread_ts", s.form(0))
	}
}

func TestPostFailsWhenTheStoredMessageDoesNotParse(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"chat.postMessage": {"": `{"ok":true,"message":{"ts":"now"}}`}})

	_, err := api.Post(context.Background(), must(t, domain.ParseConversationID, "C0001"), domain.TS{}, "hi")
	if !errors.Is(err, domain.ErrInvalidTS) || !strings.HasPrefix(err.Error(), "chat.postMessage: ") {
		t.Fatalf("Post() err = %v, want a chat.postMessage ErrInvalidTS", err)
	}
}

func TestMarkReadMovesTheMarker(t *testing.T) {
	api, s, _ := fakeSlack(t, map[string]map[string]string{"conversations.mark": {"": `{"ok":true}`}})

	if err := api.MarkRead(context.Background(), must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100")); err != nil {
		t.Fatal(err)
	}
	want := url.Values{"channel": {"C0001"}, "ts": {"1700000100.000100"}, "token": {"secret"}}
	if f := s.form(0); !reflect.DeepEqual(f, want) {
		t.Errorf("form = %v, want %v", f, want)
	}
}

func TestCallDoesNotChangeTheCallersForm(t *testing.T) {
	api, _, _ := fakeSlack(t, map[string]map[string]string{"auth.test": {"": `{"ok":true}`}})
	form := url.Values{"a": {"1"}}

	if err := api.Call(context.Background(), "auth.test", form, &Envelope{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(form, url.Values{"a": {"1"}}) {
		t.Errorf("form = %v after Call, want it unchanged", form)
	}
}

func TestSlackErrorsAreReported(t *testing.T) {
	for _, code := range authErrorCodes {
		t.Run(code, func(t *testing.T) {
			api, _, _ := fakeSlack(t, map[string]map[string]string{"auth.test": {"": `{"ok":false,"error":"` + code + `"}`}})
			err := api.Call(context.Background(), "auth.test", url.Values{}, &Envelope{})
			if !errors.Is(err, provider.ErrAuthExpired) || !strings.Contains(err.Error(), code) {
				t.Fatalf("err = %v, want ErrAuthExpired naming %s", err, code)
			}
		})
	}
	api, _, _ := fakeSlack(t, map[string]map[string]string{"auth.test": {"": `{"ok":false,"error":"ratelimited"}`}})
	err := api.Call(context.Background(), "auth.test", url.Values{}, &Envelope{})
	if errors.Is(err, provider.ErrAuthExpired) || err == nil || err.Error() != "auth.test: ratelimited" {
		t.Fatalf("err = %v, want a plain auth.test: ratelimited", err)
	}
}

func TestHTTPFailuresAreReported(t *testing.T) {
	cases := map[string]struct {
		status int
		header map[string]string
		body   string
		check  func(error) bool
	}{
		"429 with Retry-After": {http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}, "", func(err error) bool {
			var rl *provider.RateLimitedError
			return errors.As(err, &rl) && rl.Method == "auth.test" && rl.RetryAfter == 30*time.Second
		}},
		"429 without Retry-After": {http.StatusTooManyRequests, nil, "", func(err error) bool {
			var rl *provider.RateLimitedError
			return errors.As(err, &rl) && rl.RetryAfter == time.Minute
		}},
		"502": {http.StatusBadGateway, nil, "", func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "auth.test: HTTP 502")
		}},
		"not JSON": {http.StatusOK, nil, "<html>", func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "auth.test: decode response")
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := &slack{t: t}
			api, _ := s.serve(map[string]map[string]string{"auth.test": {"": tc.body}}, tc.status, tc.header)
			if err := api.Call(context.Background(), "auth.test", url.Values{}, &Envelope{}); !tc.check(err) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestEveryMethodReportsAFailedCall(t *testing.T) {
	api, _, _ := fakeSlack(t, nil) // every method answers an empty body
	ctx, conv, ts := context.Background(), must(t, domain.ParseConversationID, "C0001"), must(t, domain.ParseTS, "1700000100.000100")
	calls := map[string]func() error{
		"conversations.history": func() error { _, err := api.History(ctx, conv, ts, 1); return err },
		"chat.postMessage":      func() error { _, err := api.Post(ctx, conv, ts, "hi"); return err },
		"conversations.mark":    func() error { return api.MarkRead(ctx, conv, ts) },
	}
	for method, call := range calls {
		if err := call(); err == nil || !strings.HasPrefix(err.Error(), method+": decode response") {
			t.Errorf("%s err = %v, want its decode error", method, err)
		}
	}
}

func TestRequestsThatCannotBeSentAreErrors(t *testing.T) {
	for name, base := range map[string]string{
		"bad URL":     "http://bad\x7f/",
		"unreachable": "http://127.0.0.1:1/",
	} {
		t.Run(name, func(t *testing.T) {
			api := NewWebAPI(base, http.DefaultClient, func(url.Values, http.Header) {}, slog.Default())
			err := api.Call(context.Background(), "auth.test", url.Values{}, &Envelope{})
			if err == nil || !strings.HasPrefix(err.Error(), "auth.test: ") {
				t.Fatalf("err = %v, want an auth.test error", err)
			}
		})
	}
}

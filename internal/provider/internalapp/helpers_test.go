package internalapp

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

const (
	testUserToken = "xoxp-test-user-token"
	testAppToken  = "xapp-test-app-token"
)

func testCreds() keyring.InternalApp {
	return keyring.InternalApp{UserToken: testUserToken, AppToken: testAppToken}
}

func mustTS(t *testing.T, s string) domain.TS {
	t.Helper()
	ts, err := domain.ParseTS(s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func mustUser(t *testing.T, s string) domain.UserID {
	t.Helper()
	id, err := domain.ParseUserID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustConv(t *testing.T, s string) domain.ConversationID {
	t.Helper()
	id, err := domain.ParseConversationID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// call is one Web API request the fake Slack received.
type call struct {
	Method string
	Auth   string
	Form   url.Values
}

// fakeSlack is an httptest Web API: each method answers with its handler,
// and every request is recorded.
type fakeSlack struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []call
}

func newFakeSlack(t *testing.T, handlers map[string]http.HandlerFunc) *fakeSlack {
	t.Helper()
	f := &fakeSlack{}
	mux := http.NewServeMux()
	for method, h := range handlers {
		mux.HandleFunc("/api/"+method, func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			f.mu.Lock()
			f.calls = append(f.calls, call{Method: method, Auth: r.Header.Get("Authorization"), Form: r.PostForm})
			f.mu.Unlock()
			h(w, r)
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSlack) apiURL() string { return f.srv.URL + "/api/" }

func (f *fakeSlack) recorded() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// respond answers with status, extra headers and body.
func respond(status int, header map[string]string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

func okJSON(body []byte) http.HandlerFunc { return respond(http.StatusOK, nil, body) }

// newTestProvider returns a provider for team T0001 that talks to f and
// logs into the returned buffer.
func newTestProvider(t *testing.T, f *fakeSlack) (*Provider, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	p := New(mustTeam(t, "T0001"),
		testCreds(),
		WithAPIURL(f.apiURL()),
		WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
	)
	return p, &logs
}

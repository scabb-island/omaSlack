package desktopsession

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

// Fake credentials: tests never see a real token or cookie.
const (
	testToken  = "xoxc-test-token"
	testCookie = "xoxd-test%2Fcookie%3D"
	// testCookies is the whole session, as the browser sends it.
	testCookies = "b=test-b; d=" + testCookie + "; d-s=1700000000; x=test-x"
)

func testCreds() keyring.DesktopSession {
	return keyring.DesktopSession{Token: testToken, Cookie: testCookie, Cookies: testCookies}
}

// parse runs one of the domain parsers and fails the test on an error.
func parse[T any](t *testing.T, parser func(string) (T, error), s string) T {
	t.Helper()
	v, err := parser(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// request is what the fake Slack saw of one HTTP request.
type request struct {
	Path    string
	Header  http.Header
	Form    url.Values
	Cookie  string // the d cookie, "" when absent
	Cookies string // the whole Cookie header
}

// fakeSlack is an httptest server for both the Web API (under /api/) and
// the socket (under /ws/). It records every request.
type fakeSlack struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []request
}

func newFakeSlack(t *testing.T, routes map[string]http.HandlerFunc) *fakeSlack {
	t.Helper()
	f := &fakeSlack{}
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			rec := request{Path: r.URL.Path, Header: r.Header.Clone(), Form: r.Form, Cookies: r.Header.Get("Cookie")}
			if c, err := r.Cookie("d"); err == nil {
				rec.Cookie = c.Value
			}
			f.mu.Lock()
			f.seen = append(f.seen, rec)
			f.mu.Unlock()
			h(w, r)
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSlack) requests() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.seen...)
}

// provider returns a provider for team T0001 that talks to f and logs
// into the returned buffer.
func (f *fakeSlack) provider(t *testing.T) (*Provider, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	p := New(parse(t, domain.ParseTeamID, "T0001"), testCreds(),
		WithAPIURL(f.srv.URL+"/api/"),
		WithSocketURL("ws"+f.srv.URL[len("http"):]+"/ws/"),
		WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
	)
	return p, &logs
}

func jsonBody(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// second is the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }

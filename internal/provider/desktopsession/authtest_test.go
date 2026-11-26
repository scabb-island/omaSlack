package desktopsession

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
)

// The token and cookie are fake.
var testSession = keyring.DesktopSession{Token: "xoxc-test-token", Cookie: "xoxd-test%2Fcookie"}

// fakeAuthTest serves auth.test with body and records the request.
func fakeAuthTest(t *testing.T, status int, body string) (apiURL string, got *http.Request) {
	t.Helper()
	got = &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		*got = *r
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/api/", got
}

func TestAuthTestSendsTheSession(t *testing.T) {
	apiURL, req := fakeAuthTest(t, http.StatusOK, `{"ok":true,"url":"https://acme.slack.com/","team":"Acme","team_id":"T0ACME"}`)
	id, err := AuthTest(apiURL)(t.Context(), testSession)
	want := auth.Identity{TeamID: "T0ACME", Team: "Acme", URL: "https://acme.slack.com/"}
	if err != nil || id != want {
		t.Fatalf("AuthTest = %v, %v; want %v", id, err, want)
	}
	if req.Method != http.MethodPost || req.URL.Path != "/api/auth.test" {
		t.Errorf("request = %s %s, want POST /api/auth.test", req.Method, req.URL.Path)
	}
	if req.PostForm.Get("token") != testSession.Token {
		t.Errorf("token = %q, want the xoxc token", req.PostForm.Get("token"))
	}
	if c, err := req.Cookie("d"); err != nil || c.Value != testSession.Cookie {
		t.Errorf("d cookie = %v, %v; want it as stored", c, err)
	}
}

func TestAuthTestFailures(t *testing.T) {
	for name, tt := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"invalid session":   {http.StatusOK, `{"ok":false,"error":"invalid_auth"}`, provider.ErrAuthExpired},
		"other Slack error": {http.StatusOK, `{"ok":false,"error":"ratelimited"}`, nil},
		"HTTP error":        {http.StatusInternalServerError, ``, nil},
		"not JSON":          {http.StatusOK, `<html>`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			apiURL, _ := fakeAuthTest(t, tt.status, tt.body)
			_, err := AuthTest(apiURL)(t.Context(), testSession)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.want == nil && errors.Is(err, provider.ErrAuthExpired) {
				t.Errorf("err = %v is not an auth failure", err)
			}
			if strings.Contains(err.Error(), "xoxc-test") || strings.Contains(err.Error(), "xoxd-test") {
				t.Errorf("the error carries a secret: %v", err)
			}
		})
	}
}

func TestAuthTestUnreachable(t *testing.T) {
	if _, err := AuthTest("http://127.0.0.1:0/api/")(t.Context(), testSession); err == nil {
		t.Error("AuthTest against nothing succeeded")
	}
	if _, err := AuthTest("::bad")(t.Context(), testSession); err == nil {
		t.Error("AuthTest with a bad URL succeeded")
	}
}

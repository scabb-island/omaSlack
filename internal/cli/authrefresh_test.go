package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
	"gitlab.com/scabb-island/omaslack/internal/rpc"
	"gitlab.com/scabb-island/omaslack/internal/rpcclient"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

// refreshFixture is auth refresh against a fake Slack, a fake daemon and an
// injectable session source.
type refreshFixture struct {
	t        *testing.T
	store    keyring.ClosableStore
	sources  []auth.Source
	list     []wire.Workspace // what the daemon's workspaces.list returns
	reloads  int
	daemonUp bool
}

func newRefreshFixture(t *testing.T) *refreshFixture {
	t.Helper()
	return &refreshFixture{
		t:        t,
		store:    secretService{keyring.NewFileStore(filepath.Join(t.TempDir(), "credentials"))},
		daemonUp: true,
	}
}

// slack accepts only the fresh browser token for A3Mercados.
func (f *refreshFixture) slack(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("token") == "xoxc-fresh" {
		_, _ = w.Write([]byte(`{"ok":true,"team":"A3Mercados","team_id":"T0RSWNW0G","url":"https://matbarofex.slack.com/"}`))
		return
	}
	_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
}

func (f *refreshFixture) startDaemon() func(context.Context) (*rpcclient.Client, error) {
	router := rpc.Router{
		"workspaces.list": rpc.HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) {
			return f.list, nil
		}),
		"workspaces.reload": rpc.HandlerFunc(func(context.Context, string, json.RawMessage) (any, error) {
			f.reloads++
			return wire.ReloadAck{Reloading: true}, nil
		}),
	}
	path := filepath.Join(f.t.TempDir(), "omaslack.sock")
	l, err := rpc.Listen(context.Background(), path)
	if err != nil {
		f.t.Fatal(err)
	}
	srv := rpc.NewServer(router, rpc.Options{})
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	var once sync.Once
	f.t.Cleanup(func() { once.Do(func() { _ = srv.Close(); <-done }) })
	return func(ctx context.Context) (*rpcclient.Client, error) {
		return rpcclient.Dial(ctx, rpcclient.Options{
			Path: path, Client: "omaslack-auth-refresh",
			Run: func(context.Context, string, ...string) error { return errors.New("no systemd in tests") },
		})
	}
}

func (f *refreshFixture) run(args ...string) (int, string, string) {
	f.t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.slack))
	f.t.Cleanup(srv.Close)
	dial := f.startDaemon()
	if !f.daemonUp {
		dial = func(context.Context) (*rpcclient.Client, error) { return nil, rpcclient.ErrUnavailable }
	}
	env := Env{
		Stdout:          &bytes.Buffer{},
		Stderr:          &bytes.Buffer{},
		StdinIsTerminal: true,
		Keyring:         func(context.Context) (keyring.ClosableStore, error) { return f.store, nil },
		Sources:         func() []auth.Source { return f.sources },
		SlackAPIURL:     srv.URL + "/api/",
		DialDaemon:      dial,
	}
	var out, errb bytes.Buffer
	env.Stdout, env.Stderr = &out, &errb
	code := Run(env, append([]string{"auth", "refresh"}, args...))
	return code, out.String(), errb.String()
}

func (f *refreshFixture) setCred(id string, creds keyring.Credentials) {
	f.t.Helper()
	if err := f.store.Set(context.Background(), team(f.t, id), creds); err != nil {
		f.t.Fatal(err)
	}
}

// cliSource is a session source with fixed workspaces.
type cliSource struct{ ws []auth.Workspace }

func (s cliSource) Workspaces(context.Context) ([]auth.Workspace, error) { return s.ws, nil }

// a3Workspace is A3Mercados in a source, with the token the fake Slack
// accepts when fresh is true.
func a3Workspace(t *testing.T, fresh bool) auth.Workspace {
	t.Helper()
	token := "xoxc-stale"
	if fresh {
		token = "xoxc-fresh"
	}
	return auth.Workspace{
		Team: team(t, "T0RSWNW0G"), Name: "A3Mercados",
		Credentials: keyring.DesktopSession{Token: token, Cookie: "xoxd-c", WorkspaceURL: "https://matbarofex.slack.com/"},
	}
}

func a3Expired() wire.Workspace {
	return wire.Workspace{Team: "T0RSWNW0G", Name: "A3Mercados", Provider: "desktop-session", State: "auth_expired"}
}

func TestAuthRefreshRenewsANamedWorkspace(t *testing.T) {
	f := newRefreshFixture(t)
	f.setCred("T0RSWNW0G", keyring.DesktopSession{Token: "xoxc-stale"})
	f.sources = []auth.Source{cliSource{ws: []auth.Workspace{a3Workspace(t, true)}}}

	code, out, errOut := f.run("T0RSWNW0G")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "Refreshed T0RSWNW0G") || f.reloads != 1 {
		t.Errorf("out=%q reloads=%d", out, f.reloads)
	}
	got, _ := f.store.Get(context.Background(), team(t, "T0RSWNW0G"))
	if ds, ok := got.(keyring.DesktopSession); !ok || ds.Token != "xoxc-fresh" {
		t.Errorf("stored cred = %+v, want the fresh token", got)
	}
}

func TestAuthRefreshNoArgRefreshesExpiredDesktopSessions(t *testing.T) {
	f := newRefreshFixture(t)
	f.setCred("T0RSWNW0G", keyring.DesktopSession{Token: "xoxc-stale"})
	f.list = []wire.Workspace{
		a3Expired(),
		{Team: "T0OTHER", Provider: "internal-app", State: "auth_expired"}, // not touched
		{Team: "T0OK", Provider: "desktop-session", State: "connected"},    // not expired
	}
	f.sources = []auth.Source{cliSource{ws: []auth.Workspace{a3Workspace(t, true)}}}

	code, out, _ := f.run()
	if code != ExitOK || !strings.Contains(out, "Refreshed T0RSWNW0G") || f.reloads != 1 {
		t.Fatalf("code=%d out=%q reloads=%d", code, out, f.reloads)
	}
}

func TestAuthRefreshRejectsInternalApp(t *testing.T) {
	f := newRefreshFixture(t)
	f.setCred("T0ACME1", keyring.InternalApp{UserToken: "xoxp-u", AppToken: "xapp-a"})
	code, _, errOut := f.run("T0ACME1")
	if code == ExitOK || !strings.Contains(errOut, "internal-app") {
		t.Errorf("code=%d stderr=%q, want an error about internal-app", code, errOut)
	}
}

func TestAuthRefreshSignedOutSourceGivesHelp(t *testing.T) {
	f := newRefreshFixture(t)
	f.setCred("T0RSWNW0G", keyring.DesktopSession{Token: "xoxc-stale"})
	// The source only has a stale session the fake Slack rejects.
	f.sources = []auth.Source{cliSource{ws: []auth.Workspace{a3Workspace(t, false)}}}
	code, out, _ := f.run("T0RSWNW0G")
	if code == ExitOK {
		t.Errorf("code=%d, want an error when nothing could be refreshed", code)
	}
	if !strings.Contains(out, "Sign in to Slack again") || f.reloads != 0 {
		t.Errorf("out=%q reloads=%d", out, f.reloads)
	}
}

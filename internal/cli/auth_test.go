package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
	"gitlab.com/scabb-island/omaslack/internal/provider/desktopsession/auth"
)

// Every token, cookie and workspace below is fake.
const (
	browserToken  = "xoxc-browser-1-2"
	browserCookie = "xoxd-browser%2Fcookie"
	userToken     = "xoxp-user-1-2"
	appToken      = "xapp-app-1-2"
)

// secrets is every secret a fixture hands out; none may be printed.
var secrets = []string{"xoxc-alpha", "xoxc-beta", "xoxd-desktop", browserToken, browserCookie, userToken, appToken, "xoxc-rejected", "xoxp-rejected"}

// browserCurl is a "Copy as cURL" of a request to Slack's Web API.
const browserCurl = `curl 'https://acme.slack.com/api/client.counts' \
  -b 'b=abc; d=` + browserCookie + `; d-s=17' \
  --data-raw 'token=` + browserToken + `'
`

// secretService is a file store that says it is the Secret Service.
type secretService struct{ *keyring.FileStore }

func (secretService) Backend() keyring.Backend { return keyring.SecretService }

// authFixture is an Env with a fake Slack, a fake desktop app and a
// keyring in a temporary file.
type authFixture struct {
	t          *testing.T
	store      keyring.ClosableStore
	desktop    []auth.Workspace
	desktopErr error
	browsers   []BrowserSession
	keyringErr error
	piped      bool // stdin is not a terminal
	opened     bool
	slackCalls int
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	desk := keyring.DesktopSession{Cookie: "xoxd-desktop"}
	alpha, beta := desk, desk
	alpha.Token, beta.Token = "xoxc-alpha", "xoxc-beta"
	return &authFixture{
		t:     t,
		store: keyring.NewFileStore(filepath.Join(t.TempDir(), "credentials")),
		desktop: []auth.Workspace{
			{Team: team(t, "T0ALPHA"), Name: "Alpha", URL: "https://alpha.slack.com/", Credentials: alpha},
			{Team: team(t, "T0BETA1"), Name: "Beta", URL: "https://beta.slack.com/", Credentials: beta},
			{Team: team(t, "T0GONE1"), Name: "Gone", URL: "https://gone.slack.com/"},
		},
	}
}

func team(t *testing.T, s string) domain.TeamID {
	t.Helper()
	id, err := domain.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// slack answers auth.test: the browser session is Browser (T0BRWSR), the
// user token is Acme (T0ACME1), the desktop app's sessions are Alpha
// (T0ALPHA, which Slack answers without a name) and Beta Corp (T0BETA1),
// anything else is rejected.
func (f *authFixture) slack(w http.ResponseWriter, r *http.Request) {
	f.slackCalls++
	token := r.FormValue("token") + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	cookie, _ := r.Cookie("d")
	desktop := cookie != nil && cookie.Value == "xoxd-desktop"
	switch {
	case token == browserToken && cookie != nil && cookie.Value == browserCookie:
		_, _ = w.Write([]byte(`{"ok":true,"team":"Browser","team_id":"T0BRWSR","url":"https://browser.slack.com/"}`))
	case token == "xoxc-alpha" && desktop:
		_, _ = w.Write([]byte(`{"ok":true,"team":"","team_id":"T0ALPHA","url":"https://alpha.slack.com/"}`))
	case token == "xoxc-beta" && desktop:
		_, _ = w.Write([]byte(`{"ok":true,"team":"Beta Corp","team_id":"T0BETA1","url":"https://beta.slack.com/"}`))
	case token == userToken:
		_, _ = w.Write([]byte(`{"ok":true,"team":"Acme","team_id":"T0ACME1","url":"https://acme.slack.com/"}`))
	default:
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}
}

func (f *authFixture) env(stdin string, out, errOut *bytes.Buffer) Env {
	srv := httptest.NewServer(http.HandlerFunc(f.slack))
	f.t.Cleanup(srv.Close)
	return Env{
		Stdout: out,
		Stderr: errOut,
		Stdin:  strings.NewReader(stdin),
		// What the user types comes from a string, read as if typed at a
		// terminal unless the test says stdin is piped.
		StdinIsTerminal: !f.piped,
		Keyring: func(context.Context) (keyring.ClosableStore, error) {
			f.opened = true
			return f.store, f.keyringErr
		},
		DesktopWorkspaces: func(context.Context) ([]auth.Workspace, error) { return f.desktop, f.desktopErr },
		Browsers:          func() []BrowserSession { return f.browsers },
		SlackAPIURL:       srv.URL + "/api/",
	}
}

// run runs omaslack with stdin as what the user types, and checks that no
// secret reached the output.
func (f *authFixture) run(stdin string, args ...string) (code int, stdout, stderr string) {
	f.t.Helper()
	var out, errb bytes.Buffer
	code = Run(f.env(stdin, &out, &errb), args)
	for _, s := range secrets {
		if strings.Contains(out.String()+errb.String(), s) {
			f.t.Errorf("a secret was printed: %s\nstdout:\n%s\nstderr:\n%s", s, out.String(), errb.String())
		}
	}
	return code, out.String(), errb.String()
}

// stored returns every stored credential by team.
func (f *authFixture) stored() map[string]keyring.Credentials {
	f.t.Helper()
	ctx := context.Background()
	teams, err := f.store.Teams(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]keyring.Credentials{}
	for _, tm := range teams {
		if out[tm.String()], err = f.store.Get(ctx, tm); err != nil {
			f.t.Fatal(err)
		}
	}
	return out
}

func (f *authFixture) wantStored(want map[string]keyring.Credentials) {
	f.t.Helper()
	got := f.stored()
	if len(got) != len(want) {
		f.t.Fatalf("stored %v, want %v", got, want)
	}
	for tm, c := range want {
		if got[tm] != c {
			f.t.Errorf("%s: stored %v, want %v", tm, got[tm], c)
		}
	}
}

func wantIn(t *testing.T, where, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(text, p) {
			t.Errorf("%s does not say %q:\n%s", where, p, text)
		}
	}
}

// What auth add stores: the session with the workspace URL auth.test
// answered and, from a cURL, every cookie it carried.
var (
	alphaCreds   = keyring.DesktopSession{Token: "xoxc-alpha", Cookie: "xoxd-desktop", WorkspaceURL: "https://alpha.slack.com/"}
	betaCreds    = keyring.DesktopSession{Token: "xoxc-beta", Cookie: "xoxd-desktop", WorkspaceURL: "https://beta.slack.com/"}
	browserCreds = keyring.DesktopSession{
		Token: browserToken, Cookie: browserCookie, Cookies: "b=abc; d=" + browserCookie + "; d-s=17",
		WorkspaceURL: "https://browser.slack.com/",
	}
	// browserBareCreds is a token and a d cookie pasted apart: d alone.
	browserBareCreds = keyring.DesktopSession{Token: browserToken, Cookie: browserCookie, WorkspaceURL: "https://browser.slack.com/"}
	acmeCreds        = keyring.InternalApp{UserToken: userToken, AppToken: appToken, WorkspaceURL: "https://acme.slack.com/"}
)

func TestAuthAddDesktopDeclinedStoresNothing(t *testing.T) {
	for _, answer := range []string{"no\n", "\n", "y\n", ""} {
		f := newAuthFixture(t)
		code, out, errOut := f.run(answer, "auth", "add", "--provider", "desktop-session")
		if code != ExitError {
			t.Errorf("%q: exit code = %d, want %d", answer, code, ExitError)
		}
		wantIn(t, "stdout", out, "terms of service", "unofficial", "internal-app-setup.md", "--provider internal-app")
		wantIn(t, "stderr", errOut, "not accepted")
		if f.opened || f.slackCalls != 0 || strings.Contains(out, "Alpha") {
			t.Errorf("%q: went on after a no (keyring opened %v, %d Slack calls)", answer, f.opened, f.slackCalls)
		}
	}
}

func TestAuthAddWithoutATerminalAsksNothing(t *testing.T) {
	for _, args := range [][]string{
		{"--provider", "desktop-session"},
		{"--provider", "desktop-session", "--browser"},
		{"--provider", "internal-app"},
	} {
		f := newAuthFixture(t)
		f.piped = true
		code, out, errOut := f.run("yes\n"+browserCurl, append([]string{"auth", "add"}, args...)...)
		if code != ExitError {
			t.Errorf("%v: exit code = %d, want %d", args, code, ExitError)
		}
		wantIn(t, "stderr", errOut, "omaslack auth add needs an interactive terminal", "run it in a terminal")
		if strings.Contains(errOut, "not accepted") || out != "" {
			t.Errorf("%v: prompted anyway:\nstdout:\n%s\nstderr:\n%s", args, out, errOut)
		}
		if f.opened || f.slackCalls != 0 {
			t.Errorf("%v: went on without a terminal (keyring opened %v, %d Slack calls)", args, f.opened, f.slackCalls)
		}
	}
}

func TestAuthAddDesktopListsAndAddsThePicked(t *testing.T) {
	f := newAuthFixture(t)
	code, out, errOut := f.run("yes\n2\n", "auth", "add", "--provider", "desktop-session")
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
	wantIn(t, "stdout", out, "1. Alpha", "T0ALPHA", "2. Beta", "3. Gone", "T0GONE1", "needs sign-in", "Added Beta Corp (T0BETA1)")
	f.wantStored(map[string]keyring.Credentials{"T0BETA1": betaCreds})
	if f.slackCalls != 1 {
		t.Errorf("%d auth.test calls, want 1: the picked session is checked with Slack", f.slackCalls)
	}
}

// Desktop app credentials are checked with Slack before anything is
// stored: one Slack rejects stores nothing, even with others that pass.
func TestAuthAddDesktopRejectedBySlackStoresNothing(t *testing.T) {
	f := newAuthFixture(t)
	f.desktop[1].Credentials.Token = "xoxc-rejected"
	code, out, errOut := f.run("yes\n1 2\n", "auth", "add", "--provider", "desktop-session")
	if code != ExitError {
		t.Errorf("exit code = %d, want %d", code, ExitError)
	}
	wantIn(t, "stderr", errOut, "Beta (T0BETA1)", "invalid_auth", "Nothing was stored",
		"outside its own client", "stale", "--provider desktop-session --browser",
		"bind sessions", "--provider internal-app", "internal-app-setup.md")
	if f.opened || strings.Contains(out, "Added") {
		t.Errorf("stored after Slack rejected a session:\n%s", out)
	}
	f.wantStored(nil)
}

func TestAuthAddDesktopNamesTheWorkspaceAsSlackDoes(t *testing.T) {
	f := newAuthFixture(t)
	code, out, errOut := f.run("yes\nall\n", "auth", "add", "--provider", "desktop-session")
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
	// Slack names Beta "Beta Corp", and Alpha not at all: the desktop
	// app's name stays.
	wantIn(t, "stdout", out, "Added Alpha (T0ALPHA)", "Added Beta Corp (T0BETA1)")
}

func TestAuthAddDesktopAddsEverySignedInByDefault(t *testing.T) {
	for _, pick := range []string{"\n", "all\n", "1 2\n", "1,2\n"} {
		f := newAuthFixture(t)
		if code, _, errOut := f.run("yes\n"+pick, "auth", "add", "--provider", "desktop-session"); code != ExitOK {
			t.Fatalf("%q: exit code = %d, stderr %q", pick, code, errOut)
		}
		f.wantStored(map[string]keyring.Credentials{"T0ALPHA": alphaCreds, "T0BETA1": betaCreds})
	}
}

func TestAuthAddDesktopRejectsABadPick(t *testing.T) {
	for pick, says := range map[string]string{
		// Gone needs sign-in, and the input ends where its paste would be.
		"3\n":   "Gone (T0GONE1) needs sign-in",
		"1 3\n": "needs sign-in",
		"4\n":   `no workspace "4"`,
		"0\n":   `no workspace "0"`,
		"x\n":   `no workspace "x"`,
	} {
		f := newAuthFixture(t)
		code, _, errOut := f.run("yes\n"+pick, "auth", "add", "--provider", "desktop-session")
		if code != ExitError {
			t.Errorf("%q: exit code = %d", pick, code)
		}
		wantIn(t, "stderr", errOut, says)
		f.wantStored(nil)
	}
}

func TestAuthAddDesktopWithEveryWorkspaceSignedOut(t *testing.T) {
	f := newAuthFixture(t)
	f.desktop = f.desktop[2:]
	code, out, errOut := f.run("yes\n\n", "auth", "add", "--provider", "desktop-session")
	if code != ExitError {
		t.Errorf("exit code = %d", code)
	}
	wantIn(t, "stdout", out, "Gone", "needs sign-in")
	wantIn(t, "stderr", errOut, "no workspace to add")
	f.wantStored(nil)
}

// signedOutBrowser makes the desktop app's signed-out workspace the one
// the browser session belongs to.
func (f *authFixture) signedOutBrowser() {
	f.desktop[2].Team, f.desktop[2].Name = team(f.t, "T0BRWSR"), "Browser"
}

func TestAuthAddDesktopPickingASignedOutWorkspacePastesABrowserSessionForIt(t *testing.T) {
	for pick, want := range map[string]map[string]keyring.Credentials{
		"3\n":   {"T0BRWSR": browserCreds},
		"1 3\n": {"T0ALPHA": alphaCreds, "T0BRWSR": browserCreds},
	} {
		f := newAuthFixture(t)
		f.signedOutBrowser()
		code, out, errOut := f.run("yes\n"+pick+browserCurl, "auth", "add", "--provider", "desktop-session")
		if code != ExitOK {
			t.Fatalf("%q: exit code = %d, stderr %q", pick, code, errOut)
		}
		wantIn(t, "stdout", out, "Browser (T0BRWSR) needs sign-in", "Copy as cURL", "Added Browser (T0BRWSR)")
		f.wantStored(want)
	}
}

func TestAuthAddDesktopASessionForAnotherWorkspaceStoresNothing(t *testing.T) {
	f := newAuthFixture(t)
	code, _, errOut := f.run("yes\n1 3\n"+browserCurl, "auth", "add", "--provider", "desktop-session")
	if code != ExitError {
		t.Errorf("exit code = %d", code)
	}
	wantIn(t, "stderr", errOut, "Browser (T0BRWSR)", "not Gone (T0GONE1)")
	if f.opened {
		t.Error("opened the keyring after a session for the wrong workspace")
	}
	f.wantStored(nil)
}

func TestAuthAddDesktopASignedOutPickWithABadPasteStoresNothing(t *testing.T) {
	f := newAuthFixture(t)
	f.signedOutBrowser()
	code, _, errOut := f.run("yes\n1 3\nxoxc-rejected\n"+browserCookie+"\n", "auth", "add", "--provider", "desktop-session")
	if code != ExitError {
		t.Errorf("exit code = %d", code)
	}
	wantIn(t, "stderr", errOut, "Browser (T0BRWSR) needs sign-in", "invalid_auth")
	f.wantStored(nil)
}

// browserSessionCreds is the Browser (T0BRWSR) session as a browser source
// hands it over: the full cookie header, which Slack's auth.test accepts.
var browserSessionCreds = keyring.DesktopSession{
	Token: browserToken, Cookie: browserCookie, Cookies: "d=" + browserCookie,
	WorkspaceURL: "https://browser.slack.com/",
}

// browserSource is a detected browser whose single Slack workspace is
// Browser (T0BRWSR), or one that reports err (e.g. ErrNotSignedIn).
func browserSource(t *testing.T, name string, err error) BrowserSession {
	t.Helper()
	return BrowserSession{Name: name, Workspaces: func(context.Context) ([]auth.Workspace, error) {
		if err != nil {
			return nil, err
		}
		return []auth.Workspace{{
			Team: team(t, "T0BRWSR"), Name: "Browser", URL: "https://browser.slack.com/",
			Credentials: browserSessionCreds,
		}}, nil
	}}
}

// Spec slack-connection, "Browser session read automatically": a browser is
// the only source, so it is read without a source menu.
func TestAuthAddReadsABrowserWhenItIsTheOnlySource(t *testing.T) {
	f := newAuthFixture(t)
	f.desktopErr = auth.ErrDesktopNotFound
	f.browsers = []BrowserSession{browserSource(t, "Brave", nil)}
	code, out, errOut := f.run("yes\nall\n", "auth", "add", "--provider", "desktop-session")
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
	wantIn(t, "stdout", out, "Workspaces in Brave:", "1. Browser", "T0BRWSR", "Added Browser (T0BRWSR)")
	f.wantStored(map[string]keyring.Credentials{"T0BRWSR": browserSessionCreds})
}

// With the desktop app and a browser both signed in, the user chooses.
func TestAuthAddChoosesBetweenSources(t *testing.T) {
	f := newAuthFixture(t)
	f.browsers = []BrowserSession{browserSource(t, "Brave", nil)}
	code, out, errOut := f.run("yes\n2\nall\n", "auth", "add", "--provider", "desktop-session")
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
	wantIn(t, "stdout", out, "more than one place", "1. the Slack desktop app (3 workspaces)",
		"2. Brave (1 workspace)", "Workspaces in Brave:", "Added Browser (T0BRWSR)")
	f.wantStored(map[string]keyring.Credentials{"T0BRWSR": browserSessionCreds})
}

// A browser that is installed but signed out of Slack is not a source.
func TestAuthAddSkipsASignedOutBrowser(t *testing.T) {
	f := newAuthFixture(t)
	f.browsers = []BrowserSession{browserSource(t, "Brave", auth.ErrNotSignedIn)}
	code, out, errOut := f.run("yes\n2\n", "auth", "add", "--provider", "desktop-session")
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
	// Only the desktop app is a source, so it is read straight away.
	wantIn(t, "stdout", out, "Workspaces in the Slack desktop app:", "Added Beta Corp (T0BETA1)")
	if strings.Contains(out, "more than one place") || strings.Contains(out, "Brave") {
		t.Errorf("offered a signed-out browser:\n%s", out)
	}
}

// Spec slack-connection, "A browser session Slack no longer accepts".
func TestAuthAddBrowserRejectedBySlackGivesTheBoundHelp(t *testing.T) {
	f := newAuthFixture(t)
	f.desktopErr = auth.ErrDesktopNotFound
	rejected := browserSessionCreds
	rejected.Token = "xoxc-rejected"
	f.browsers = []BrowserSession{{Name: "Brave", Workspaces: func(context.Context) ([]auth.Workspace, error) {
		return []auth.Workspace{{Team: team(t, "T0BRWSR"), Name: "Browser", Credentials: rejected}}, nil
	}}}
	code, _, errOut := f.run("yes\nall\n", "auth", "add", "--provider", "desktop-session")
	if code != ExitError {
		t.Errorf("exit code = %d, want %d", code, ExitError)
	}
	wantIn(t, "stderr", errOut, "invalid_auth", "bind sessions", "--provider internal-app")
	f.wantStored(nil)
}

// Spec slack-connection, "No desktop app installed".
func TestAuthAddDesktopFallsBackToABrowserPaste(t *testing.T) {
	f := newAuthFixture(t)
	f.desktopErr = auth.ErrDesktopNotFound
	code, out, errOut := f.run("yes\n"+browserCurl, "auth", "add", "--provider", "desktop-session")
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
	wantIn(t, "stdout", out, auth.ErrDesktopNotFound.Error(), "Copy as cURL", "Added Browser (T0BRWSR)")
	f.wantStored(map[string]keyring.Credentials{"T0BRWSR": browserCreds})
}

func TestAuthAddDesktopBrowserOnRequest(t *testing.T) {
	for name, in := range map[string]struct {
		stdin string
		args  []string
	}{
		"picked b":  {"yes\nb\n" + browserCurl, nil},
		"--browser": {"yes\n" + browserCurl, []string{"--browser"}},
	} {
		f := newAuthFixture(t)
		args := append([]string{"auth", "add", "--provider", "desktop-session"}, in.args...)
		if code, _, errOut := f.run(in.stdin, args...); code != ExitOK {
			t.Fatalf("%s: exit code = %d, stderr %q", name, code, errOut)
		}
		f.wantStored(map[string]keyring.Credentials{"T0BRWSR": browserCreds})
	}
}

func TestAuthAddBrowserAsksForTheCookieApart(t *testing.T) {
	f := newAuthFixture(t)
	code, out, errOut := f.run("yes\n"+browserToken+"\n"+browserCookie+"\n", "auth", "add", "--provider", "desktop-session", "--browser")
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
	wantIn(t, "stdout", out, "d cookie")
	f.wantStored(map[string]keyring.Credentials{"T0BRWSR": browserBareCreds})
}

func TestAuthAddBrowserFailures(t *testing.T) {
	for paste, says := range map[string]string{
		"curl 'https://app.slack.com/client'\n":               "no xoxc- token",
		browserToken + "\nnot a cookie\n":                     "no d cookie",
		"xoxc-rejected\n" + browserCookie + "\n":              "invalid_auth",
		"curl -b 'd=xoxd-x' --data-raw 'token=xoxc-1' \\\x03": "cancelled",
	} {
		f := newAuthFixture(t)
		code, _, errOut := f.run("yes\n"+paste, "auth", "add", "--provider", "desktop-session", "--browser")
		if code != ExitError {
			t.Errorf("%q: exit code = %d", paste, code)
		}
		wantIn(t, "stderr", errOut, says)
		f.wantStored(nil)
	}
}

// A session pasted fresh from a browser that Slack rejects is bound to the
// browser: pasting another will not help, internal-app will.
func TestAuthAddBrowserRejectedBySlackPointsToInternalApp(t *testing.T) {
	f := newAuthFixture(t)
	code, _, errOut := f.run("yes\nxoxc-rejected\n"+browserCookie+"\n", "auth", "add", "--provider", "desktop-session", "--browser")
	if code != ExitError {
		t.Errorf("exit code = %d, want %d", code, ExitError)
	}
	wantIn(t, "stderr", errOut, "invalid_auth", "Nothing was stored", "outside its own client",
		"bind sessions", "--provider internal-app", "internal-app-setup.md")
	if strings.Contains(errOut, "--browser") {
		t.Errorf("suggests pasting another browser session:\n%s", errOut)
	}
	f.wantStored(nil)
}

// Only Slack's rejection gets the explanation; a paste with no session
// in it is the user's mistake.
func TestAuthAddBadPasteIsNotExplainedAsARejection(t *testing.T) {
	f := newAuthFixture(t)
	_, _, errOut := f.run("yes\ncurl 'https://app.slack.com/client'\n", "auth", "add", "--provider", "desktop-session", "--browser")
	wantIn(t, "stderr", errOut, "no xoxc- token")
	if strings.Contains(errOut, "internal-app") {
		t.Errorf("explains a bad paste as a rejection:\n%s", errOut)
	}
}

func TestAuthAddInternalApp(t *testing.T) {
	f := newAuthFixture(t)
	code, out, errOut := f.run(userToken+"\n"+appToken+"\n", "auth", "add", "--provider", "internal-app")
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
	wantIn(t, "stdout", out, "xoxp-", "xapp-", "Added Acme (T0ACME1)", "internal-app")
	f.wantStored(map[string]keyring.Credentials{"T0ACME1": acmeCreds})
}

func TestAuthAddInternalAppFailures(t *testing.T) {
	for stdin, says := range map[string]string{
		appToken + "\n" + appToken + "\n":   "user token must start with xoxp-",
		userToken + "\n" + userToken + "\n": "app-level token must start with xapp-",
		"xoxp-rejected\n" + appToken + "\n": "invalid_auth",
		"":                                  "user token must start with xoxp-",
	} {
		f := newAuthFixture(t)
		code, _, errOut := f.run(stdin, "auth", "add", "--provider", "internal-app")
		if code != ExitError {
			t.Errorf("exit code = %d", code)
		}
		wantIn(t, "stderr", errOut, says)
		f.wantStored(nil)
	}
}

func TestAuthAddReportsAKeyringItCannotOpen(t *testing.T) {
	f := newAuthFixture(t)
	f.keyringErr = errors.New("keyring is locked")
	code, _, errOut := f.run(userToken+"\n"+appToken+"\n", "auth", "add", "--provider", "internal-app")
	if code != ExitError {
		t.Errorf("exit code = %d", code)
	}
	wantIn(t, "stderr", errOut, "keyring is locked")
}

func TestAuthAddSaysWhereTheCredentialsAre(t *testing.T) {
	f := newAuthFixture(t)
	_, out, _ := f.run(userToken+"\n"+appToken+"\n", "auth", "add", "--provider", "internal-app")
	wantIn(t, "stdout", out, "file", fileBackendWarning)
	f = newAuthFixture(t)
	f.store = secretService{keyring.NewFileStore(filepath.Join(t.TempDir(), "credentials"))}
	_, out, _ = f.run(userToken+"\n"+appToken+"\n", "auth", "add", "--provider", "internal-app")
	wantIn(t, "stdout", out, "secret-service")
	if strings.Contains(out, fileBackendWarning) {
		t.Errorf("warns about a file with the Secret Service:\n%s", out)
	}
}

func (f *authFixture) seed() {
	f.t.Helper()
	ctx := context.Background()
	for tm, c := range map[string]keyring.Credentials{
		"T0BETA1": betaCreds,
		"T0ACME1": keyring.InternalApp{UserToken: userToken, AppToken: appToken},
	} {
		if err := f.store.Set(ctx, team(f.t, tm), c); err != nil {
			f.t.Fatal(err)
		}
	}
}

func TestAuthList(t *testing.T) {
	f := newAuthFixture(t)
	f.seed()
	code, out, errOut := f.run("", "auth", "list")
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
	acme, beta := strings.Index(out, "T0ACME1"), strings.Index(out, "T0BETA1")
	if acme < 0 || beta < acme {
		t.Errorf("teams missing or out of order:\n%s", out)
	}
	wantIn(t, "stdout", out, "T0ACME1      internal-app\n", "T0BETA1      desktop-session\n", "file", fileBackendWarning)
}

func TestAuthListSecretServiceHasNoWarning(t *testing.T) {
	f := newAuthFixture(t)
	f.store = secretService{keyring.NewFileStore(filepath.Join(t.TempDir(), "credentials"))}
	f.seed()
	_, out, _ := f.run("", "auth", "list")
	wantIn(t, "stdout", out, "T0ACME1", "secret-service")
	if strings.Contains(out, fileBackendWarning) {
		t.Errorf("warns about a file with the Secret Service:\n%s", out)
	}
}

func TestAuthListEmpty(t *testing.T) {
	f := newAuthFixture(t)
	code, out, _ := f.run("", "auth", "list")
	if code != ExitOK {
		t.Errorf("exit code = %d", code)
	}
	wantIn(t, "stdout", out, "No workspaces", "omaslack auth add")
}

func TestAuthListReportsAKeyringItCannotOpen(t *testing.T) {
	f := newAuthFixture(t)
	f.keyringErr = errors.New("no keyring")
	if code, _, errOut := f.run("", "auth", "list"); code != ExitError || !strings.Contains(errOut, "no keyring") {
		t.Errorf("exit code = %d, stderr %q", code, errOut)
	}
}

func TestAuthRemove(t *testing.T) {
	f := newAuthFixture(t)
	f.seed()
	code, out, _ := f.run("", "auth", "remove", "T0BETA1")
	if code != ExitOK {
		t.Errorf("exit code = %d", code)
	}
	wantIn(t, "stdout", out, "Removed T0BETA1")
	f.wantStored(map[string]keyring.Credentials{"T0ACME1": keyring.InternalApp{UserToken: userToken, AppToken: appToken}})
}

func TestAuthRemoveUnknownIsNotAnError(t *testing.T) {
	f := newAuthFixture(t)
	code, out, _ := f.run("", "auth", "remove", "T0NOPE1")
	if code != ExitOK {
		t.Errorf("exit code = %d", code)
	}
	wantIn(t, "stdout", out, "No credentials stored for T0NOPE1")
}

func TestAuthRemoveReportsAKeyringItCannotOpen(t *testing.T) {
	f := newAuthFixture(t)
	f.keyringErr = errors.New("no keyring")
	if code, _, errOut := f.run("", "auth", "remove", "T0BETA1"); code != ExitError || !strings.Contains(errOut, "no keyring") {
		t.Errorf("exit code = %d, stderr %q", code, errOut)
	}
}

func TestAuthUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"auth"},
		{"auth", "frob"},
		{"auth", "add"},
		{"auth", "add", "--provider", "carrier-pigeon"},
		{"auth", "add", "--provider", "internal-app", "extra"},
		{"auth", "add", "--provider", "internal-app", "--browser"},
		{"auth", "add", "--nope"},
		{"auth", "list", "extra"},
		{"auth", "remove"},
		{"auth", "remove", "T0BETA1", "T0ACME1"},
		{"auth", "remove", "not-a-team"},
	} {
		f := newAuthFixture(t)
		code, _, errOut := f.run("", args...)
		if code != ExitUsage {
			t.Errorf("%v: exit code = %d, want %d", args, code, ExitUsage)
		}
		wantIn(t, "stderr", errOut, "Usage: omaslack auth")
		if f.opened {
			t.Errorf("%v: opened the keyring", args)
		}
	}
}

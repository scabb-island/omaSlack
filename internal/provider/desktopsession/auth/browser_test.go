package auth

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

// browserConfigFixture is a browser's localConfig_v2: a signed-in
// workspace carries its name and url (there is no root-state.json), one
// has an empty token and is skipped, and one id does not parse.
const browserConfigFixture = `{"teams":{` +
	`"T06M3KK7Y":{"token":"xoxc-brave","name":"Scabb Island","url":"https://scabbisland.slack.com/"},` +
	`"T0EMPTY":{"token":"","name":"Empty","url":"https://empty.slack.com/"},` +
	`"not-a-team":{"token":"xoxc-bad","name":"Bad","url":"https://bad.slack.com/"}}}`

// buildBrowserProfile writes a fake Chromium profile at dir with the given
// localConfig_v2 value and cookies, and returns a Browser that reads it
// with a fake keyring key.
func buildBrowserProfile(t *testing.T, dir, cookiesName string, config string, cookies ...cookieRow) Browser {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir()) // temporary copies land here
	writeLevelDB(t, filepath.Join(dir, "Local Storage", "leveldb"), append([]byte{0x01}, config...))
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, cookiesName)), 0o700); err != nil {
		t.Fatal(err)
	}
	writeCookieDB(t, filepath.Join(dir, cookiesName), cookies...)
	return Browser{
		Name: "Brave", Dir: dir,
		Passwords: &passwords{keys: []string{"keyring-key"}},
		Log:       slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}

func scabbCookie(t *testing.T) cookieRow {
	t.Helper()
	return cookieRow{host: ".slack.com", name: "d", encrypted: encryptCookie(t, "v11", []byte("xoxd-brave"), "keyring-key")}
}

func TestBrowserWorkspacesReadsTheSession(t *testing.T) {
	b := buildBrowserProfile(t, filepath.Join(t.TempDir(), "Default"), "Cookies", browserConfigFixture,
		scabbCookie(t),
		cookieRow{host: ".slack.com", name: "x", encrypted: encryptCookie(t, "v11", []byte("x-val"), "keyring-key")},
	)
	got, err := b.Workspaces(t.Context())
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	want := []Workspace{{
		Team: mustTeam(t, "T06M3KK7Y"), Name: "Scabb Island", URL: "https://scabbisland.slack.com/",
		Credentials: keyring.DesktopSession{
			Token: "xoxc-brave", Cookie: "xoxd-brave",
			Cookies: "d=xoxd-brave; x=x-val", WorkspaceURL: "https://scabbisland.slack.com/",
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Workspaces =\n %+v\nwant\n %+v", got, want)
	}
}

func TestBrowserWorkspacesReadsNetworkCookies(t *testing.T) {
	b := buildBrowserProfile(t, filepath.Join(t.TempDir(), "Default"), filepath.Join("Network", "Cookies"),
		browserConfigFixture, scabbCookie(t))
	got, err := b.Workspaces(t.Context())
	if err != nil || len(got) != 1 || got[0].Credentials.Cookie != "xoxd-brave" {
		t.Fatalf("Workspaces = %+v, %v", got, err)
	}
}

func TestBrowserWorkspacesNotSignedIn(t *testing.T) {
	b := Browser{Name: "Brave", Dir: filepath.Join(t.TempDir(), "Default"), Passwords: &passwords{}}
	if _, err := b.Workspaces(t.Context()); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("Workspaces error = %v, want ErrNotSignedIn", err)
	}
}

func TestBrowserWorkspacesWithoutTheKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Default")
	b := buildBrowserProfile(t, dir, "Cookies", browserConfigFixture, scabbCookie(t))
	b.Passwords = &passwords{keys: []string{"wrong-key"}}
	if _, err := b.Workspaces(t.Context()); !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("Workspaces error = %v, want ErrDecryptFailed", err)
	}
}

func TestFindBrowsersDetectsInstalledProfiles(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir()) // no ~/.config browsers
	for _, p := range []string{
		filepath.Join("BraveSoftware", "Brave-Browser", "Default"),
		filepath.Join("chromium", "Default"),
	} {
		if err := os.MkdirAll(filepath.Join(cfg, p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	for _, b := range FindBrowsers() {
		names = append(names, b.Name)
	}
	if !reflect.DeepEqual(names, []string{"Brave", "Chromium"}) {
		t.Errorf("FindBrowsers names = %v, want [Brave Chromium]", names)
	}
}

func TestFindBrowsersDeduplicatesAcrossConfigHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")) // same as ~/.config
	if err := os.MkdirAll(filepath.Join(home, ".config", "chromium", "Default"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := FindBrowsers(); len(got) != 1 || got[0].Name != "Chromium" {
		t.Errorf("FindBrowsers = %+v, want one Chromium", got)
	}
}

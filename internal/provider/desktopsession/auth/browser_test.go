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

// profileWithStorage makes a browser profile directory with a Local
// Storage leveldb, so FindBrowsers treats it as a profile.
func profileWithStorage(t *testing.T, dir string) {
	t.Helper()
	writeLevelDB(t, filepath.Join(dir, "Local Storage", "leveldb"), append([]byte{0x01}, `{"teams":{}}`...))
}

func TestFindBrowsersDetectsInstalledProfiles(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir()) // no ~/.config browsers
	t.Setenv("TMPDIR", t.TempDir())
	profileWithStorage(t, filepath.Join(cfg, "BraveSoftware", "Brave-Browser", "Default"))
	profileWithStorage(t, filepath.Join(cfg, "chromium", "Default"))
	var names []string
	for _, b := range FindBrowsers() {
		names = append(names, b.Name)
	}
	if !reflect.DeepEqual(names, []string{"Brave", "Chromium"}) {
		t.Errorf("FindBrowsers names = %v, want [Brave Chromium]", names)
	}
}

func TestFindBrowsersReadsEveryProfile(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	root := filepath.Join(cfg, "BraveSoftware", "Brave-Browser")
	profileWithStorage(t, filepath.Join(root, "Default"))
	profileWithStorage(t, filepath.Join(root, "Profile 1"))
	profileWithStorage(t, filepath.Join(root, "Profile 2"))
	// Skipped: System Profile, and a profile with no Local Storage.
	profileWithStorage(t, filepath.Join(root, "System Profile"))
	if err := os.MkdirAll(filepath.Join(root, "Profile 3"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "Local State"),
		`{"profile":{"info_cache":{"Default":{"name":"Personal"},"Profile 1":{"name":"Primary"}}}}`)
	var names []string
	for _, b := range FindBrowsers() {
		names = append(names, b.Name)
	}
	// Default first and plain; non-Default named by its display name, or
	// the directory when Local State has none.
	if !reflect.DeepEqual(names, []string{"Brave", "Brave (Primary)", "Brave (Profile 2)"}) {
		t.Errorf("FindBrowsers names = %v", names)
	}
}

func TestFindBrowsersDeduplicatesAcrossConfigHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")) // same as ~/.config
	t.Setenv("TMPDIR", t.TempDir())
	profileWithStorage(t, filepath.Join(home, ".config", "chromium", "Default"))
	if got := FindBrowsers(); len(got) != 1 || got[0].Name != "Chromium" {
		t.Errorf("FindBrowsers = %+v, want one Chromium", got)
	}
}

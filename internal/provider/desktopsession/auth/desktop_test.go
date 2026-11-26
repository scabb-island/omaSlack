// Adapted from github.com/gammons/slk internal/slackdesktop/configdir_test.go, workspaces_test.go, localstorage_test.go and cookiedb_test.go (MIT, Copyright (c) 2026 Grant Ammons).

package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

// The workspace names and domains below are made up.
const rootStateFixture = `{
  "workspaces": {
    "T0BETA1": {"name": "Beta", "url": "https://beta.slack.com/", "domain": "beta", "id": "T0BETA1"},
    "T0ALPHA": {"name": "Alpha", "url": "https://alpha.slack.com/", "domain": "alpha", "id": "T0ALPHA"},
    "E0GRID1": {"name": "Grid", "url": "https://grid.enterprise.slack.com/", "domain": "grid", "id": "E0GRID1"},
    "T0NOTOKEN": {"name": "No token", "url": "https://notoken.slack.com/", "domain": "notoken", "id": "T0NOTOKEN"},
    "broken": {"name": "Broken", "url": "https://broken.slack.com/", "id": "not-a-team"},
    "T0NOURL": {"name": "No URL", "id": "T0NOURL"}
  }
}`

const localConfigFixture = `{"teams":{"T0ALPHA":{"token":"xoxc-alpha"},"T0BETA1":{"token":"xoxc-beta"},"E0GRID1":{"token":"xoxc-grid"},"T0NOURL":{"token":"xoxc-nourl"},"T0EMPTY":{"token":""}}}`

// desktopFixture builds a fake Slack config directory: the workspace list,
// Local Storage with the tokens and a Cookies database.
type desktopFixture struct {
	dir       string
	rootState string
	tokens    []byte // localConfig_v2 value, tag byte included; nil for no LevelDB
	cookie    cookieRow
	others    []cookieRow // the session's other cookies
	logs      bytes.Buffer
}

type cookieRow struct {
	host, name, value string
	encrypted         []byte
}

func newDesktopFixture(t *testing.T) *desktopFixture {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir()) // the reader's temporary copies land here
	return &desktopFixture{
		dir:       filepath.Join(t.TempDir(), "Slack"),
		rootState: rootStateFixture,
		tokens:    append([]byte{0x01}, localConfigFixture...),
		cookie:    cookieRow{host: ".slack.com", name: "d", encrypted: encryptCookie(t, "v10", []byte("xoxd-desktop"), "peanuts")},
	}
}

func (f *desktopFixture) build(t *testing.T) Desktop {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(f.dir, "storage"), 0o700); err != nil {
		t.Fatal(err)
	}
	if f.rootState != "" {
		writeTestFile(t, filepath.Join(f.dir, "storage", "root-state.json"), f.rootState)
	}
	if f.tokens != nil {
		writeLevelDB(t, filepath.Join(f.dir, "Local Storage", "leveldb"), f.tokens)
	}
	if f.cookie.name != "" {
		writeCookieDB(t, filepath.Join(f.dir, "Cookies"), append([]cookieRow{f.cookie}, f.others...)...)
	}
	return Desktop{
		Dir: f.dir, Passwords: &passwords{keys: []string{"keyring-key"}},
		Log: slog.New(slog.NewTextHandler(&f.logs, nil)),
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeLevelDB mimics Chromium's Local Storage: keys are
// "_<origin>\x00\x01<key>", values carry a one-byte encoding tag.
func writeLevelDB(t *testing.T, dir string, localConfigValue []byte) {
	t.Helper()
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for key, value := range map[string][]byte{
		"_https://app.slack.com\x00\x01localConfig_v2":     localConfigValue,
		"_https://app.slack.com\x00\x01other":              {0x01, '{', '}'},
		"_https://app.slack.com\x00\x01bad_localConfig_v2": {0x01, 'x'},
	} {
		if err := db.Put([]byte(key), value, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func writeCookieDB(t *testing.T, path string, rows ...cookieRow) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, encrypted_value BLOB)`,
		`INSERT INTO cookies VALUES ('.slack.com', 'd-s', '', x'00')`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(t.Context(), s); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range rows {
		if _, err := db.ExecContext(t.Context(), `INSERT INTO cookies VALUES (?, ?, ?, ?)`, row.host, row.name, row.value, row.encrypted); err != nil {
			t.Fatal(err)
		}
	}
}

func mustTeam(t *testing.T, s string) domain.TeamID {
	t.Helper()
	id, err := domain.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDesktopWorkspacesJoinsTheAppsState(t *testing.T) {
	d := newDesktopFixture(t).build(t)
	got, err := d.Workspaces(context.Background())
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	creds := func(token, url string) keyring.DesktopSession {
		return keyring.DesktopSession{Token: token, Cookie: "xoxd-desktop", Cookies: "d=xoxd-desktop", WorkspaceURL: url}
	}
	want := []Workspace{
		{
			Team: mustTeam(t, "T0ALPHA"), Name: "Alpha", URL: "https://alpha.slack.com/",
			Credentials: creds("xoxc-alpha", "https://alpha.slack.com/"),
		},
		{
			Team: mustTeam(t, "T0BETA1"), Name: "Beta", URL: "https://beta.slack.com/",
			Credentials: creds("xoxc-beta", "https://beta.slack.com/"),
		},
		{
			Team: mustTeam(t, "E0GRID1"), Name: "Grid", URL: "https://grid.enterprise.slack.com/",
			Credentials: creds("xoxc-grid", "https://grid.enterprise.slack.com/"),
		},
		{Team: mustTeam(t, "T0NOTOKEN"), Name: "No token", URL: "https://notoken.slack.com/"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Workspaces =\n%v\nwant\n%v", got, want)
	}
	for _, w := range got {
		if w.NeedsSignIn() != (w.Name == "No token") {
			t.Errorf("%s: NeedsSignIn = %v", w.Name, w.NeedsSignIn())
		}
	}
}

// A workspace the app lists but holds no token for (signed out, or never
// opened on this machine) comes back without credentials, so the CLI can
// say it needs sign-in. With no token at all the cookie is not needed.
func TestDesktopWorkspacesWithoutTokensNeedSignIn(t *testing.T) {
	for name, mutate := range map[string]func(*desktopFixture){
		"no Local Storage": func(f *desktopFixture) { f.tokens = nil },
		"no tokens":        func(f *desktopFixture) { f.tokens = []byte{0x01, '{', '}'} },
		"no usable tokens": func(f *desktopFixture) {
			f.tokens = append([]byte{0x01}, `{"teams":{"broken":{"token":"xoxc-x"},"T0ALPHA":{"token":""}}}`...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDesktopFixture(t)
			mutate(f)
			f.cookie = cookieRow{}
			got, err := f.build(t).Workspaces(context.Background())
			if err != nil || len(got) != 4 {
				t.Fatalf("Workspaces = %v, %v; want the 4 listed ones", got, err)
			}
			for _, w := range got {
				if !w.NeedsSignIn() || w.Credentials != (keyring.DesktopSession{}) {
					t.Errorf("%s: NeedsSignIn = %v, credentials %v", w.Name, w.NeedsSignIn(), w.Credentials)
				}
			}
		})
	}
}

// Slack keeps its profile open while running; the reader works on copies
// and leaves the profile exactly as it was.
func TestDesktopWorkspacesLeavesTheProfileUntouched(t *testing.T) {
	f := newDesktopFixture(t)
	d := f.build(t)
	before := listTree(t, f.dir)
	if _, err := d.Workspaces(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after := listTree(t, f.dir); !reflect.DeepEqual(after, before) {
		t.Errorf("profile changed:\nbefore %v\nafter  %v", before, after)
	}
	if left, _ := os.ReadDir(os.TempDir()); len(left) != 0 {
		t.Errorf("temporary copies left behind: %v", left)
	}
}

func listTree(t *testing.T, root string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		info, err := e.Info()
		out[path] = info.Size()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDesktopWorkspacesUsesAPlaintextCookie(t *testing.T) {
	f := newDesktopFixture(t)
	f.cookie = cookieRow{host: ".slack.com", name: "d", value: "xoxd-plain"}
	got, err := f.build(t).Workspaces(context.Background())
	if err != nil || got[0].Credentials.Cookie != "xoxd-plain" {
		t.Fatalf("Workspaces = %v, %v", got, err)
	}
}

func TestDesktopWorkspacesDecryptsWithTheKeyring(t *testing.T) {
	f := newDesktopFixture(t)
	f.cookie.encrypted = encryptCookie(t, "v11", []byte("xoxd-from-keyring"), "keyring-key")
	got, err := f.build(t).Workspaces(context.Background())
	if err != nil || got[0].Credentials.Cookie != "xoxd-from-keyring" {
		t.Fatalf("Workspaces = %v, %v", got, err)
	}
}

func TestDesktopWorkspacesFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*desktopFixture)
		want   error
	}{
		{"no workspace list", func(f *desktopFixture) { f.rootState = "" }, ErrNotSignedIn},
		{"malformed workspace list", func(f *desktopFixture) { f.rootState = "{" }, ErrNotSignedIn},
		{"no workspaces", func(f *desktopFixture) { f.rootState = `{"workspaces":{}}` }, ErrNotSignedIn},
		{"no cookie database", func(f *desktopFixture) { f.cookie = cookieRow{} }, ErrCookieDBMissing},
		{"no d cookie", func(f *desktopFixture) { f.cookie.name = "x" }, ErrCookieDBMissing},
		{"cookie of another host", func(f *desktopFixture) { f.cookie.host = ".example.com" }, ErrCookieDBMissing},
		{"undecryptable cookie", func(f *desktopFixture) { f.cookie.encrypted = []byte("v10abc") }, ErrDecryptFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newDesktopFixture(t)
			tt.mutate(f)
			if _, err := f.build(t).Workspaces(context.Background()); !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

// A profile that is there but cannot be read is not "not signed in". The
// failures are a directory where a file belongs and the reverse, which hold
// even for root (CI runs tests as root, where chmod 0 blocks nothing).
func TestDesktopWorkspacesReportsAnUnreadableProfile(t *testing.T) {
	f := newDesktopFixture(t)
	f.cookie = cookieRow{}
	d := f.build(t)
	if err := os.Mkdir(filepath.Join(f.dir, "Cookies"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Workspaces(context.Background()); err == nil || errors.Is(err, ErrCookieDBMissing) {
		t.Errorf("error = %v, want a read failure", err)
	}
	ls := filepath.Join(f.dir, "Local Storage", "leveldb")
	if err := os.RemoveAll(ls); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, ls, "")
	if _, err := d.Workspaces(context.Background()); err == nil || errors.Is(err, ErrNotSignedIn) {
		t.Errorf("error = %v, want a read failure", err)
	}
}

func TestDecodeLocalStorageValue(t *testing.T) {
	u16 := utf16.Encode([]rune(`{"b":"ñ"}`)) // non-ASCII on purpose: UTF-16 is how Chromium stores it
	utf16Value := []byte{0x00}
	for _, c := range u16 {
		utf16Value = append(utf16Value, byte(c), byte(c>>8))
	}
	for _, tt := range []struct {
		in   []byte
		want string
	}{
		{nil, ""},
		{append([]byte{0x01}, `{"a":1}`...), `{"a":1}`},
		{utf16Value, `{"b":"ñ"}`},
		{[]byte(`{"c":3}`), `{"c":3}`},
	} {
		if got := decodeLocalStorageValue(tt.in); got != tt.want {
			t.Errorf("decodeLocalStorageValue(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFindDesktopDir(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if _, err := FindDesktopDir(); !errors.Is(err, ErrDesktopNotFound) {
		t.Fatalf("FindDesktopDir with no app: %v, want ErrDesktopNotFound", err)
	}
	// Each install location wins over the ones after it.
	for _, dir := range []string{
		filepath.Join(home, "snap", "slack", "current", ".config", "Slack"),
		filepath.Join(home, ".var", "app", "com.slack.Slack", "config", "Slack"),
		filepath.Join(home, ".config", "Slack"),
		filepath.Join(xdg, "Slack"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if got, err := FindDesktopDir(); err != nil || got != dir {
			t.Errorf("FindDesktopDir() = %q, %v; want %q", got, err, dir)
		}
	}
	// A file where the directory should be is not an install.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	writeTestFile(t, filepath.Join(os.Getenv("HOME"), ".config"), "")
	if _, err := FindDesktopDir(); !errors.Is(err, ErrDesktopNotFound) {
		t.Errorf("FindDesktopDir over a file: %v, want ErrDesktopNotFound", err)
	}
}

// Some workspaces reject the d cookie alone (GitLab issue #1), so every
// cookie the app holds for the workspace's address goes with it: those
// for .slack.com and those for its own host. One that cannot be decrypted
// is skipped and named in the log, without its value; the keyring is asked
// for its key once.
func TestDesktopWorkspacesKeepEverySessionCookie(t *testing.T) {
	f := newDesktopFixture(t)
	f.others = []cookieRow{
		{host: ".slack.com", name: "b", value: "b-plain"},
		{host: ".slack.com", name: "x", encrypted: encryptCookie(t, "v11", []byte("x-from-keyring"), "keyring-key")},
		{host: ".slack.com", name: "lc", encrypted: encryptCookie(t, "v11", []byte("lc-from-keyring"), "keyring-key")},
		{host: "alpha.slack.com", name: "own", encrypted: encryptCookie(t, "v10", alphaHashed(), "peanuts")},
		{host: "beta.slack.com", name: "other", value: "beta-only"},
		{host: "slack.com", name: "root", value: "slack-com-only"},
		{host: ".example.com", name: "z", value: "elsewhere"},
		{host: ".slack.com", name: "broken", encrypted: []byte("v10abc")},
	}
	d := f.build(t)
	got, err := d.Workspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cookies := map[string]string{}
	for _, w := range got {
		cookies[w.Name] = w.Credentials.Cookies
	}
	want := map[string]string{
		"Alpha":    "d=xoxd-desktop; b=b-plain; lc=lc-from-keyring; x=x-from-keyring; own=alpha-own",
		"Beta":     "d=xoxd-desktop; b=b-plain; lc=lc-from-keyring; x=x-from-keyring; other=beta-only",
		"Grid":     "d=xoxd-desktop; b=b-plain; lc=lc-from-keyring; x=x-from-keyring",
		"No token": "",
	}
	if !reflect.DeepEqual(cookies, want) {
		t.Errorf("cookies =\n%v\nwant\n%v", cookies, want)
	}
	if asked := d.Passwords.(*passwords).calls; asked != 1 {
		t.Errorf("the keyring was asked %d times, want once", asked)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "broken") || !strings.Contains(logs, "d-s") {
		t.Errorf("logs = %q, want the undecryptable cookies named", logs)
	}
	if strings.Contains(logs, "v10abc") {
		t.Errorf("logs = %q carry a cookie value", logs)
	}
}

// alphaHashed is a cookie value for alpha.slack.com behind the hash of
// its host, as Chromium 130 and later store it.
func alphaHashed() []byte {
	sum := sha256.Sum256([]byte("alpha.slack.com"))
	return append(sum[:], "alpha-own"...)
}

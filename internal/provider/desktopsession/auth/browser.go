package auth

import (
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

// chromiumBrowsers lists the Chromium-based browsers whose Slack web
// session omaslack can reuse. subdir is the browser's directory under the
// config home; keyringApp names its "Safe Storage" cookie key (the Secret
// Service "application" attribute is the browser's lowercase name, the
// KWallet folder and entry use its product name).
var chromiumBrowsers = []struct {
	name   string
	subdir string
	app    keyringApp
}{
	{"Brave", filepath.Join("BraveSoftware", "Brave-Browser"), keyringApp{secretAttr: "brave", kwalletProduct: "Brave"}},
	{"Google Chrome", "google-chrome", keyringApp{secretAttr: "chrome", kwalletProduct: "Chrome"}},
	{"Chromium", "chromium", keyringApp{secretAttr: "chromium", kwalletProduct: "Chromium"}},
	{"Microsoft Edge", "microsoft-edge", keyringApp{secretAttr: "microsoft-edge", kwalletProduct: "Microsoft Edge"}},
}

// Browser reads the Slack web session of one Chromium browser's default
// profile, the same way Desktop reads the Slack desktop app: the xoxc
// token from Local Storage and the cookies from the profile's Cookies
// database, decrypted with the browser's Safe Storage key. Passwords
// supplies that key; a nil one uses the real keyring for this browser.
// Log names cookies that cannot be decrypted; nil means slog.Default().
type Browser struct {
	Name      string
	Dir       string // the profile directory, e.g. ".../Brave-Browser/Default"
	Passwords PasswordSource
	Log       *slog.Logger

	app keyringApp
}

// FindBrowsers returns a Browser for every profile of every Chromium
// browser that is installed, in catalog order, Default profile first. It
// does not read the profiles' sessions, so a profile that is signed out of
// Slack is still returned; Workspaces reports that with ErrNotSignedIn. A
// non-Default profile is named after the browser and its display name, for
// example "Brave (Primary)".
func FindBrowsers() []Browser {
	var found []Browser
	seen := map[string]bool{}
	for _, base := range configHomes() {
		for _, b := range chromiumBrowsers {
			root := filepath.Join(base, b.subdir)
			names := profileNames(root)
			for _, dir := range profileDirs(root) {
				key := b.name + "\x00" + dir
				if seen[key] {
					continue
				}
				seen[key] = true
				name := b.name
				if dir != "Default" {
					name += " (" + cmp.Or(names[dir], dir) + ")"
				}
				found = append(found, Browser{Name: name, Dir: filepath.Join(root, dir), app: b.app})
			}
		}
	}
	return found
}

// profileDirs lists the profile directories of a browser root that hold a
// Local Storage (Default first, then "Profile N" in order), skipping the
// System and Guest profiles, which never hold a user's Slack session.
func profileDirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if isProfileDir(root, e) {
			dirs = append(dirs, e.Name())
		}
	}
	slices.SortFunc(dirs, defaultFirst)
	return dirs
}

// isProfileDir reports a browser profile directory that holds a Slack-able
// Local Storage: Default or "Profile N", never the System or Guest profile.
func isProfileDir(root string, e os.DirEntry) bool {
	name := e.Name()
	if !e.IsDir() || name == "System Profile" || name == "Guest Profile" {
		return false
	}
	if name != "Default" && !strings.HasPrefix(name, "Profile ") {
		return false
	}
	_, err := os.Stat(filepath.Join(root, name, "Local Storage", "leveldb"))
	return err == nil
}

// defaultFirst orders the Default profile before the others, then by name.
func defaultFirst(a, b string) int {
	if (a == "Default") != (b == "Default") {
		if a == "Default" {
			return -1
		}
		return 1
	}
	return cmp.Compare(a, b)
}

// profileNames maps each profile directory to its display name, from the
// browser's Local State. An unreadable Local State leaves the map empty,
// and callers fall back to the directory name.
func profileNames(root string) map[string]string {
	data, err := os.ReadFile(filepath.Join(root, "Local State")) //nolint:gosec // the browser's own file under HOME
	if err != nil {
		return nil
	}
	var state struct {
		Profile struct {
			InfoCache map[string]struct {
				Name string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	names := make(map[string]string, len(state.Profile.InfoCache))
	for dir, info := range state.Profile.InfoCache {
		if info.Name != "" {
			names[dir] = info.Name
		}
	}
	return names
}

// configHomes is where browser profiles live: $XDG_CONFIG_HOME first when
// set, then ~/.config.
func configHomes() []string {
	home := filepath.Join(os.Getenv("HOME"), ".config")
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" && xdg != home {
		return []string{xdg, home}
	}
	return []string{home}
}

// Workspaces lists every workspace the browser is signed in to, each with
// the credentials the desktop-session provider needs: the xoxc token, the
// session's d cookie and every other cookie a browser would send to the
// workspace's address. The profile is only read, through temporary copies,
// so it is safe to call while the browser is open. A browser with no Slack
// session reports ErrNotSignedIn.
func (b Browser) Workspaces(ctx context.Context) ([]Workspace, error) {
	sessions, err := readSessions(filepath.Join(b.Dir, "Local Storage", "leveldb"))
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, ErrNotSignedIn
	}
	cookies, err := readCookies(ctx, b.cookiesPath(), b.passwords(), cmp.Or(b.Log, slog.Default()))
	if err != nil {
		return nil, err
	}
	out := make([]Workspace, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, Workspace{
			Team: s.Team,
			Name: s.Name,
			URL:  s.URL,
			Credentials: keyring.DesktopSession{
				Token:        s.Token,
				Cookie:       cookies.d,
				Cookies:      cookies.header(hostOf(s.URL)),
				WorkspaceURL: s.URL,
			},
		})
	}
	return out, nil
}

// SignedIn reports whether the browser profile holds a Slack web session
// (an xoxc token), without touching the keyring. It is for a quick
// availability check such as omaslack doctor; reading the session still
// needs the cookie key, so a true here is not a promise the session works.
func (b Browser) SignedIn() bool {
	sessions, err := readSessions(filepath.Join(b.Dir, "Local Storage", "leveldb"))
	return err == nil && len(sessions) > 0
}

func (b Browser) passwords() PasswordSource {
	if b.Passwords != nil {
		return b.Passwords
	}
	return SystemPasswords{App: b.app}
}

// cookiesPath is where the browser keeps its Cookies database: the newer
// Network/Cookies, or Cookies directly in the profile for older versions.
func (b Browser) cookiesPath() string {
	network := filepath.Join(b.Dir, "Network", "Cookies")
	if _, err := os.Stat(network); err == nil {
		return network
	}
	return filepath.Join(b.Dir, "Cookies")
}

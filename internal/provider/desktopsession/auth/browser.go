package auth

import (
	"cmp"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

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

// FindBrowsers returns a Browser for every Chromium browser whose default
// profile exists, in catalog order. It does not read the profiles, so a
// browser that is installed but signed out of Slack is still returned;
// Workspaces reports that with ErrNotSignedIn.
func FindBrowsers() []Browser {
	var found []Browser
	for _, base := range configHomes() {
		for _, b := range chromiumBrowsers {
			profile := filepath.Join(base, b.subdir, "Default")
			if info, err := os.Stat(profile); err != nil || !info.IsDir() {
				continue
			}
			if hasBrowser(found, b.name) {
				continue
			}
			found = append(found, Browser{Name: b.name, Dir: profile, app: b.app})
		}
	}
	return found
}

func hasBrowser(found []Browser, name string) bool {
	return slices.ContainsFunc(found, func(b Browser) bool { return b.Name == name })
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

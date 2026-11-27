// Adapted from github.com/gammons/slk internal/slackdesktop/configdir.go, workspaces.go, cookie.go and cmd/slk/onboarding_core.go (MIT, Copyright (c) 2026 Grant Ammons).

// Package auth finds the credentials of the desktop-session provider: the
// xoxc token and xoxd "d" cookie of a Slack session. It reads them from
// the Slack desktop app's profile (Desktop.Workspaces) or from a request
// copied from a browser as cURL (ParseCurl, FromCurl). It never talks to
// Slack itself: FromCurl takes the auth.test call as a parameter.
package auth

import (
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/keyring"
)

// Workspace is one workspace a Slack session is signed in to, with the
// credentials the desktop-session provider needs for it.
type Workspace struct {
	Team        domain.TeamID
	Name        string
	URL         string
	Credentials keyring.DesktopSession
}

// NeedsSignIn reports a workspace the desktop app lists but holds no
// token for: it has no credentials until the user signs in to it again.
func (w Workspace) NeedsSignIn() bool { return w.Credentials.Token == "" }

// FindDesktopDir returns the Slack desktop app's config directory, the
// first that exists of: $XDG_CONFIG_HOME/Slack, ~/.config/Slack, the
// Flatpak one and the Snap one.
func FindDesktopDir() (string, error) {
	home := os.Getenv("HOME")
	candidates := []string{
		filepath.Join(home, ".config", "Slack"),
		filepath.Join(home, ".var", "app", "com.slack.Slack", "config", "Slack"),
		filepath.Join(home, "snap", "slack", "current", ".config", "Slack"),
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append([]string{filepath.Join(xdg, "Slack")}, candidates...)
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() { //nolint:gosec // fixed locations under HOME, which the user controls
			return dir, nil
		}
	}
	return "", ErrDesktopNotFound
}

// Desktop reads the session of the Slack desktop app whose config
// directory is Dir. Passwords supplies the key its cookies are encrypted
// with; SystemPasswords{} is the real one. Log names the cookies that
// cannot be decrypted; nil means slog.Default().
type Desktop struct {
	Dir       string
	Passwords PasswordSource
	Log       *slog.Logger
}

// Workspaces lists every workspace the desktop app is signed in to, sorted
// by name. Those it holds a token for share the app's "d" cookie and get
// every other cookie a browser would send to their address (some
// workspaces reject the d cookie alone), and their address as the
// workspace URL; the others come back without credentials (see
// NeedsSignIn). Slack's profile is only read, through temporary copies, so
// it is safe to call while Slack runs.
func (d Desktop) Workspaces(ctx context.Context) ([]Workspace, error) {
	listed, err := readWorkspaceList(filepath.Join(d.Dir, "storage", "root-state.json"))
	if err != nil {
		return nil, err
	}
	tokens, err := readTokens(filepath.Join(d.Dir, "Local Storage", "leveldb"))
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return listed, nil
	}
	cookies, err := readCookies(ctx, filepath.Join(d.Dir, "Cookies"), d.Passwords, cmp.Or(d.Log, slog.Default()))
	if err != nil {
		return nil, err
	}
	for i, w := range listed {
		if token := tokens[w.Team]; token != "" {
			listed[i].Credentials = keyring.DesktopSession{
				Token: token, Cookie: cookies.d, Cookies: cookies.header(hostOf(w.URL)), WorkspaceURL: w.URL,
			}
		}
	}
	return listed, nil
}

// SignedIn reports whether the desktop app holds a Slack web session (an
// xoxc token), without touching the keyring. Like Browser.SignedIn, it is
// for a quick availability check, not a promise the session still works.
func (d Desktop) SignedIn() bool {
	tokens, err := readTokens(filepath.Join(d.Dir, "Local Storage", "leveldb"))
	return err == nil && len(tokens) > 0
}

// hostOf is the host of a workspace URL, or "" when it has none; with no
// host, a workspace still gets the .slack.com cookies.
func hostOf(workspaceURL string) string {
	u, err := url.Parse(workspaceURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// rootState is the part of storage/root-state.json that lists the
// signed-in workspaces.
type rootState struct {
	Workspaces map[string]struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		ID   string `json:"id"`
	} `json:"workspaces"`
}

// readWorkspaceList parses the workspace list once, keeping the entries
// with a valid team ID and a URL.
func readWorkspaceList(path string) ([]Workspace, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is the desktop app's own file
	if err != nil {
		return nil, ErrNotSignedIn
	}
	var rs rootState
	if json.Unmarshal(data, &rs) != nil {
		return nil, ErrNotSignedIn
	}
	var out []Workspace
	for _, w := range rs.Workspaces {
		team, err := domain.ParseTeamID(w.ID)
		if err == nil && w.URL != "" {
			out = append(out, Workspace{Team: team, Name: w.Name, URL: w.URL})
		}
	}
	if len(out) == 0 {
		return nil, ErrNotSignedIn
	}
	slices.SortFunc(out, func(a, b Workspace) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

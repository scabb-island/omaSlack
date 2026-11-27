// Package theme holds the TUI's colors (spec tui-client, "Omarchy theme
// with live reload"; task 3.6). Every style in the TUI derives from a
// Theme: no other package hard-codes a color.
package theme

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"

	"charm.land/lipgloss/v2"
)

// Theme is the palette the TUI draws with. The names are roles, not
// colors, so any Omarchy theme maps onto them.
type Theme struct {
	Background color.Color // pane backgrounds
	Foreground color.Color // normal text
	Muted      color.Color // timestamps, hints, read conversations
	Accent     color.Color // focused pane border, selection, links
	Border     color.Color // unfocused pane borders
	Selection  color.Color // background of the selected row
	Unread     color.Color // conversations with unread messages
	Mention    color.Color // mentions of the user, mention badges
	Code       color.Color // inline code and code blocks
	Quote      color.Color // block quote bar
	Success    color.Color // connected state
	Warning    color.Color // connecting, rate limited
	Error      color.Color // disconnected, auth expired, failed sends
}

// Default is the palette used when no Omarchy theme can be read: dark
// background, Tokyo Night-like accents.
func Default() Theme {
	c := lipgloss.Color
	return Theme{
		Background: c("#1a1b26"), Foreground: c("#c0caf5"), Muted: c("#565f89"),
		Accent: c("#7aa2f7"), Border: c("#3b4261"), Selection: c("#283457"),
		Unread: c("#e0af68"), Mention: c("#f7768e"), Code: c("#9ece6a"),
		Quote: c("#bb9af7"), Success: c("#9ece6a"), Warning: c("#e0af68"), Error: c("#f7768e"),
	}
}

// Paths are where Load looks, in order: the rendered omaslack.toml, then
// the active theme's colors.toml.
type Paths struct {
	OmaslackTOML string // ~/.local/state/omarchy/current/theme/omaslack.toml
	ColorsTOML   string // ~/.local/state/omarchy/current/theme/colors.toml
}

// ErrNoTheme means neither file exists; callers use Default.
var ErrNoTheme = errors.New("theme: no Omarchy theme found")

// DefaultPaths resolves Paths from $HOME (task 3.6). It ignores
// $XDG_STATE_HOME on purpose: Omarchy always renders its themes under
// $HOME/.local/state/omarchy, whatever XDG says.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("theme: %w", err)
	}
	return PathsFrom(home)
}

// PathsFrom resolves Paths under home; an empty home is an error.
func PathsFrom(home string) (Paths, error) {
	if home == "" {
		return Paths{}, errors.New("theme: no home directory")
	}
	dir := filepath.Join(home, ".local", "state", "omarchy", "current", "theme")
	return Paths{
		OmaslackTOML: filepath.Join(dir, "omaslack.toml"),
		ColorsTOML:   filepath.Join(dir, "colors.toml"),
	}, nil
}

// Load reads the theme from p (task 3.6): omaslack.toml if it exists,
// else colors.toml mapped onto the roles (see roles). A role the file does
// not set keeps its Default value; a set but invalid color is an error
// naming the file and the key. ErrNoTheme means neither file exists.
func Load(p Paths) (Theme, error) {
	if t, err := loadFile(p.OmaslackTOML, omaslackKeys); !errors.Is(err, fs.ErrNotExist) {
		return t, err
	}
	if t, err := loadFile(p.ColorsTOML, colorsKeys); !errors.Is(err, fs.ErrNotExist) {
		return t, err
	}
	return Theme{}, ErrNoTheme
}

func loadFile(path string, keys func(role) []string) (Theme, error) {
	if path == "" {
		return Theme{}, fs.ErrNotExist
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own theme file
	if err != nil {
		return Theme{}, err
	}
	t, err := fromValues(parseFlatTOML(string(data)), keys)
	if err != nil {
		return Theme{}, fmt.Errorf("theme: %s: %w", path, err)
	}
	return t, nil
}

// fromValues starts from Default and sets each role from the first of its
// keys that values holds.
func fromValues(values map[string]string, keys func(role) []string) (Theme, error) {
	t := Default()
	for _, r := range roles {
		k, v, ok := firstSet(values, keys(r))
		if !ok {
			continue
		}
		c, err := parseColor(v)
		if err != nil {
			return Theme{}, fmt.Errorf("%s: %w", k, err)
		}
		*r.field(&t) = c
	}
	return t, nil
}

func firstSet(values map[string]string, keys []string) (key, value string, ok bool) {
	for _, k := range keys {
		if v, ok := values[k]; ok {
			return k, v, true
		}
	}
	return "", "", false
}

// Watch reloads the theme on every signal received on sigs (SIGUSR1 in
// production) and sends each successfully loaded theme on the returned
// channel, which closes when ctx ends (task 3.6). A failed reload is
// logged by the caller-supplied load and skipped.
func Watch(ctx context.Context, load func() (Theme, error), sigs <-chan os.Signal) <-chan Theme {
	ch := make(chan Theme)
	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigs:
			}
			t, err := load()
			if err != nil {
				continue
			}
			select {
			case ch <- t:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

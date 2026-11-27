// Package theme holds the TUI's colors (spec tui-client, "Omarchy theme
// with live reload"; task 3.6). Every style in the TUI derives from a
// Theme: no other package hard-codes a color.
package theme

import (
	"context"
	"errors"
	"image/color"
	"os"

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

// DefaultPaths resolves Paths from $HOME (task 3.6).
func DefaultPaths() (Paths, error) { return Paths{}, errTODO }

// Load reads the theme from p (task 3.6).
func Load(p Paths) (Theme, error) { return Theme{}, errTODO }

// Watch reloads the theme on every signal received on sigs (SIGUSR1 in
// production) and sends each successfully loaded theme on the returned
// channel, which closes when ctx ends (task 3.6). A failed reload is
// logged by the caller-supplied load and skipped.
func Watch(ctx context.Context, load func() (Theme, error), sigs <-chan os.Signal) <-chan Theme {
	ch := make(chan Theme)
	go func() { <-ctx.Done(); close(ch) }()
	return ch
}

var errTODO = errors.New("theme: not implemented yet (task 3.6)")

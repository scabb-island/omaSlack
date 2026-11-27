package theme

import (
	"errors"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// role is one Theme field and the keys that set it. In omaslack.toml the
// key is the role's own name. In Omarchy's colors.toml it is the first key
// present in colors: the semantic name first, then the legacy ANSI name
// Omarchy aliases to it (omarchy-theme-color does the same aliasing when
// it renders templates).
type role struct {
	name   string
	colors []string
	field  func(*Theme) *color.Color
}

// roles is the colors.toml → Theme mapping. It is the only place the
// mapping lives; packaging/omarchy/omaslack.toml.tpl must agree with it.
var roles = []role{
	{"background", []string{"background", "color0"}, func(t *Theme) *color.Color { return &t.Background }},
	{"foreground", []string{"foreground", "color7"}, func(t *Theme) *color.Color { return &t.Foreground }},
	{"muted", []string{"muted", "color8"}, func(t *Theme) *color.Color { return &t.Muted }},
	{"accent", []string{"accent", "blue", "color4"}, func(t *Theme) *color.Color { return &t.Accent }},
	{"border", []string{"lighter_background"}, func(t *Theme) *color.Color { return &t.Border }},
	{"selection", []string{"selection", "selection_background"}, func(t *Theme) *color.Color { return &t.Selection }},
	{"unread", []string{"yellow", "color3"}, func(t *Theme) *color.Color { return &t.Unread }},
	{"mention", []string{"red", "color1"}, func(t *Theme) *color.Color { return &t.Mention }},
	{"code", []string{"green", "color2"}, func(t *Theme) *color.Color { return &t.Code }},
	{"quote", []string{"magenta", "color5"}, func(t *Theme) *color.Color { return &t.Quote }},
	{"success", []string{"green", "color2"}, func(t *Theme) *color.Color { return &t.Success }},
	{"warning", []string{"yellow", "color3"}, func(t *Theme) *color.Color { return &t.Warning }},
	{"error", []string{"red", "color1"}, func(t *Theme) *color.Color { return &t.Error }},
}

func omaslackKeys(r role) []string { return []string{r.name} }

func colorsKeys(r role) []string { return r.colors }

// parseFlatTOML reads the flat `key = "value"` lines Omarchy's theme files
// use. Comments, blank lines, sections and any line that is not a quoted
// string assignment are skipped: only the keys a role names are checked.
func parseFlatTOML(s string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		if k, v, ok := parseAssignment(line); ok {
			values[k] = v
		}
	}
	return values
}

func parseAssignment(line string) (key, value string, ok bool) {
	key, rest, found := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	rest = strings.TrimSpace(rest)
	if !found || key == "" || strings.HasPrefix(key, "#") || len(rest) < 2 {
		return "", "", false
	}
	q := rest[:1]
	end := strings.Index(rest[1:], q)
	if (q != `"` && q != "'") || end < 0 {
		return "", "", false
	}
	return key, rest[1 : end+1], true
}

var errColor = errors.New(`want a hex color like "#1a1b26"`)

// parseColor accepts #rrggbb, with or without the '#' (Omarchy's
// {{ name_strip }} variables drop it).
func parseColor(s string) (color.Color, error) {
	h := strings.TrimPrefix(s, "#")
	if len(h) != 6 || strings.Trim(strings.ToLower(h), "0123456789abcdef") != "" {
		return nil, errColor
	}
	return lipgloss.Color("#" + h), nil
}

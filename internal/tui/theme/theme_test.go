package theme

import (
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func hex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return string([]byte{'#',
		"0123456789abcdef"[r>>12], "0123456789abcdef"[(r>>8)&15],
		"0123456789abcdef"[g>>12], "0123456789abcdef"[(g>>8)&15],
		"0123456789abcdef"[b>>12], "0123456789abcdef"[(b>>8)&15]})
}

func TestPathsFromHome(t *testing.T) {
	p, err := PathsFrom("/home/ana")
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		OmaslackTOML: "/home/ana/.local/state/omarchy/current/theme/omaslack.toml",
		ColorsTOML:   "/home/ana/.local/state/omarchy/current/theme/colors.toml",
	}
	if p != want {
		t.Fatalf("paths = %+v", p)
	}
	if _, err := PathsFrom(""); err == nil {
		t.Fatal("empty home: want an error")
	}
}

func TestDefaultPathsUsesHome(t *testing.T) {
	t.Setenv("HOME", "/home/bo")
	p, err := DefaultPaths()
	if err != nil || !strings.HasPrefix(p.ColorsTOML, "/home/bo/.local/state/omarchy/") {
		t.Fatalf("paths = %+v, %v", p, err)
	}
}

func TestLoadWithoutFilesIsErrNoTheme(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(Paths{OmaslackTOML: filepath.Join(dir, "a"), ColorsTOML: filepath.Join(dir, "b")})
	if !errors.Is(err, ErrNoTheme) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadPrefersOmaslackTOML(t *testing.T) {
	dir := t.TempDir()
	p := Paths{
		OmaslackTOML: write(t, dir, "omaslack.toml", `
# rendered by Omarchy
background = "#000001"
accent     = "#00000a" # inline comment
error      = '#00000b'
`),
		ColorsTOML: write(t, dir, "colors.toml", `accent = "#ffffff"`),
	}
	th, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if hex(th.Background) != "#000001" || hex(th.Accent) != "#00000a" || hex(th.Error) != "#00000b" {
		t.Fatalf("theme = %s %s %s", hex(th.Background), hex(th.Accent), hex(th.Error))
	}
	if hex(th.Mention) != hex(Default().Mention) {
		t.Fatalf("missing role must fall back to Default, got %s", hex(th.Mention))
	}
}

func TestLoadMapsColorsTOML(t *testing.T) {
	dir := t.TempDir()
	p := Paths{OmaslackTOML: filepath.Join(dir, "missing"), ColorsTOML: write(t, dir, "colors.toml", `
mode = "dark"
hyprland_active_border = "#a295cb #89d4cf 45deg"
background = "#101010"
foreground = "#202020"
muted = "#303030"
accent = "#404040"
lighter_background = "#505050"
selection = "#606060"
red = "#700000"
green = "#007000"
yellow = "#707000"
magenta = "#700070"
`)}
	th, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]color.Color{
		"#101010": th.Background, "#202020": th.Foreground, "#303030": th.Muted,
		"#404040": th.Accent, "#505050": th.Border, "#606060": th.Selection,
		"#700000": th.Mention, "#007000": th.Code, "#707000": th.Unread, "#700070": th.Quote,
	}
	for want, c := range got {
		if hex(c) != want {
			t.Errorf("want %s, got %s", want, hex(c))
		}
	}
	if hex(th.Error) != "#700000" || hex(th.Success) != "#007000" || hex(th.Warning) != "#707000" {
		t.Errorf("status colors = %s %s %s", hex(th.Error), hex(th.Success), hex(th.Warning))
	}
}

func TestLoadMapsLegacyANSIColors(t *testing.T) {
	dir := t.TempDir()
	p := Paths{OmaslackTOML: filepath.Join(dir, "missing"), ColorsTOML: write(t, dir, "colors.toml", `
color0 = "#000000"
color1 = "#110000"
color4 = "#000044"
color7 = "#777777"
color8 = "#888888"
`)}
	th, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if hex(th.Background) != "#000000" || hex(th.Mention) != "#110000" || hex(th.Accent) != "#000044" ||
		hex(th.Foreground) != "#777777" || hex(th.Muted) != "#888888" {
		t.Fatalf("theme = %+v", th)
	}
}

func TestLoadRejectsInvalidColorNamingTheKey(t *testing.T) {
	dir := t.TempDir()
	p := Paths{OmaslackTOML: write(t, dir, "omaslack.toml", `unread = "#12345"`)}
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "unread") || !strings.Contains(err.Error(), "omaslack.toml") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadReportsUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(Paths{OmaslackTOML: dir}); err == nil || errors.Is(err, ErrNoTheme) {
		t.Fatalf("a directory is not a theme: err = %v", err)
	}
}

// The Omarchy template and the colors.toml mapping must agree: each role
// renders from the first colors.toml key the fallback mapping reads.
func TestTemplateMatchesRoles(t *testing.T) {
	data, err := os.ReadFile("../../../packaging/omarchy/omaslack.toml.tpl")
	if err != nil {
		t.Fatal(err)
	}
	values := parseFlatTOML(string(data))
	if len(values) != len(roles) {
		t.Errorf("template sets %d keys, want %d roles", len(values), len(roles))
	}
	for _, r := range roles {
		if want := "{{ " + r.colors[0] + " }}"; values[r.name] != want {
			t.Errorf("template %s = %q, want %q", r.name, values[r.name], want)
		}
	}
}

func TestParseColor(t *testing.T) {
	for in, ok := range map[string]bool{
		"#a1B2c3": true, "a1b2c3": true, "#a1b2c3ff": false, "#abc": false, "": false, "#gggggg": false,
	} {
		if _, err := parseColor(in); (err == nil) != ok {
			t.Errorf("parseColor(%q) err = %v", in, err)
		}
	}
}

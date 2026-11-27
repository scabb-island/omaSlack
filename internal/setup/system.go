package setup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"gitlab.com/scabb-island/omaslack/internal/doctor"
)

// ShareDir is where the package installs the files setup copies (spec
// aur-packaging, "Installed file layout").
const ShareDir = "/usr/share/omaslack"

// DefaultLayout resolves the layout for the user in sys's environment,
// with the packaged files in source: ShareDir, or a checkout's packaging/
// directory, whose files have their source names (omarchy/...), so a
// development build can be set up without the package.
//
// The targets follow Omarchy, which reads its templates, hooks and
// Hyprland modules from $HOME/.config whatever $XDG_CONFIG_HOME says. The
// manifest and the data follow $XDG_STATE_HOME and $XDG_DATA_HOME.
func DefaultLayout(sys System, source string) (Layout, error) {
	home := sys.Getenv("HOME")
	if home == "" {
		return Layout{}, errors.New("HOME is not set")
	}
	src := installedSources
	if _, err := sys.FS.ReadFile(filepath.Join(source, checkoutSources[0])); err == nil {
		src = checkoutSources
	}
	paths := doctor.PathsFor(home, "")
	hypr := filepath.Join(home, ".config", "hypr")
	return Layout{
		Template:   Item{"theme template", filepath.Join(source, src[0]), paths.Template, 0o644},
		Hook:       Item{"theme hook", filepath.Join(source, src[1]), paths.Hook, 0o755},
		WindowRule: Item{"window rule", filepath.Join(source, src[2]), filepath.Join(hypr, "omaslack.lua"), 0o644},
		Manifest:   filepath.Join(xdg(sys.Getenv, "XDG_STATE_HOME", home, ".local/state"), "omaslack", "setup.json"),
		Rendered:   paths.Rendered,
		HyprConfig: filepath.Join(hypr, "hyprland.lua"),
		Data:       filepath.Join(xdg(sys.Getenv, "XDG_DATA_HOME", home, ".local/share"), "omaslack"),
	}, nil
}

// The template, the hook and the window rule, as the package installs
// them and as a checkout's packaging/ directory holds them.
var (
	installedSources = [3]string{"omaslack.toml.tpl", "theme-set.hook", "hypr/omaslack.lua"}
	checkoutSources  = [3]string{"omarchy/omaslack.toml.tpl", "omarchy/theme-set.d/omaslack", "omarchy/hypr/omaslack.lua"}
)

// xdg is the XDG base directory in env var, or home/fallback when it is
// unset or empty.
func xdg(getenv func(string) string, env, home, fallback string) string {
	if dir := getenv(env); dir != "" {
		return dir
	}
	return filepath.Join(home, fallback)
}

// OS is this machine.
func OS() System {
	return System{FS: OSFS{}, Exec: output, Getenv: os.Getenv}
}

func output(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // fixed commands: systemctl, omarchy-theme-refresh
	if err != nil && len(out) > 0 {
		return string(out), errors.New(err.Error() + ": " + string(out))
	}
	return string(out), err
}

// OSFS is the real filesystem.
type OSFS struct{}

// ReadFile reads path.
func (OSFS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) } //nolint:gosec // setup's own paths

// Remove removes path.
func (OSFS) Remove(path string) error { return os.Remove(path) }

// WriteFile writes data to a temporary file next to path and renames it
// over path, so a crash never leaves a half-written file.
func (OSFS) WriteFile(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // ~/.config directories are 0755
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op after the rename
	if err := writeAndClose(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func writeAndClose(f *os.File, data []byte, mode fs.FileMode) error {
	_, err := f.Write(data)
	if err == nil {
		err = f.Chmod(mode)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

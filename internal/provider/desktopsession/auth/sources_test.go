package auth

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// signedInToken writes a Local Storage leveldb with one workspace token at
// profile/Local Storage/leveldb, so SignedIn reports true.
func signedInToken(t *testing.T, profile string) {
	t.Helper()
	writeLevelDB(t, filepath.Join(profile, "Local Storage", "leveldb"),
		append([]byte{0x01}, `{"teams":{"T06M3KK7Y":{"token":"xoxc-x"}}}`...))
}

func TestSignedInSourcesNamesTheDesktopAppAndBrowsers(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir()) // no ~/.config sources
	t.Setenv("TMPDIR", t.TempDir())
	signedInToken(t, filepath.Join(cfg, "Slack"))
	signedInToken(t, filepath.Join(cfg, "BraveSoftware", "Brave-Browser", "Default"))
	// Chromium is installed but signed out: present but not a source.
	if err := os.MkdirAll(filepath.Join(cfg, "chromium", "Default"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := SignedInSources(); !reflect.DeepEqual(got, []string{DesktopSourceName, "Brave"}) {
		t.Errorf("SignedInSources = %v, want [%q Brave]", got, DesktopSourceName)
	}
}

func TestSignedInSourcesEmptyWhenNothingSignedIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if got := SignedInSources(); len(got) != 0 {
		t.Errorf("SignedInSources = %v, want none", got)
	}
}

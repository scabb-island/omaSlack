package keyring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

func team(t *testing.T, s string) domain.TeamID {
	t.Helper()
	id, err := domain.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newTestFileStore(t *testing.T) (*FileStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "omaslack", "credentials")
	return NewFileStore(path), path
}

func TestFileStoreReportsItsBackend(t *testing.T) {
	s, _ := newTestFileStore(t)
	if s.Backend() != File {
		t.Errorf("Backend() = %q, want %q", s.Backend(), File)
	}
}

func TestFileStoreGetBeforeAnySetIsNotFound(t *testing.T) {
	s, _ := newTestFileStore(t)
	_, err := s.Get(context.Background(), team(t, "T1A"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get error = %v, want ErrNotFound", err)
	}
	teams, err := s.Teams(context.Background())
	if err != nil || len(teams) != 0 {
		t.Errorf("Teams() = %v, %v; want none", teams, err)
	}
}

func TestFileStoreKeepsOneSetPerTeam(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestFileStore(t)
	desktop := DesktopSession{Token: "xoxc-1", Cookie: "xoxd-1"}
	app := InternalApp{UserToken: "xoxp-1", AppToken: "xapp-1"}
	mustSet(t, s, team(t, "T2B"), desktop)
	mustSet(t, s, team(t, "T1A"), DesktopSession{Token: "old"})
	mustSet(t, s, team(t, "T1A"), app)

	assertGet(t, s, team(t, "T2B"), desktop)
	assertGet(t, s, team(t, "T1A"), app)
	teams, err := s.Teams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []domain.TeamID{team(t, "T1A"), team(t, "T2B")}; !reflect.DeepEqual(teams, want) {
		t.Errorf("Teams() = %v, want %v", teams, want)
	}
}

func TestFileStoreDeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestFileStore(t)
	if err := s.Delete(ctx, team(t, "T1A")); err != nil {
		t.Fatalf("Delete with no file: %v", err)
	}
	mustSet(t, s, team(t, "T1A"), DesktopSession{Token: "xoxc-1"})
	mustSet(t, s, team(t, "T2B"), DesktopSession{Token: "xoxc-2"})
	for range 2 {
		if err := s.Delete(ctx, team(t, "T1A")); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}
	if _, err := s.Get(ctx, team(t, "T1A")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete error = %v, want ErrNotFound", err)
	}
	assertGet(t, s, team(t, "T2B"), DesktopSession{Token: "xoxc-2"})
}

func TestFileStoreFileIsPrivate(t *testing.T) {
	s, path := newTestFileStore(t)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustSet(t, s, team(t, "T1A"), DesktopSession{Token: "xoxc-1"})

	assertMode(t, path, 0o600)
	assertMode(t, dir, 0o700|os.ModeDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the credentials file", len(entries))
	}
}

func TestFileStoreRejectsAMalformedFile(t *testing.T) {
	ctx := context.Background()
	for name, content := range map[string]string{
		"not json":     `{`,
		"bad provider": `{"teams":{"T1A":{"provider":"carrier-pigeon"}}}`,
		"bad team":     `{"teams":{"not-a-team":{"provider":"desktop-session"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, path := newTestFileStore(t)
			writeFile(t, path, content)
			_, getErr := s.Get(ctx, team(t, "T1A"))
			_, teamsErr := s.Teams(ctx)
			setErr := s.Set(ctx, team(t, "T2B"), DesktopSession{})
			for _, err := range []error{getErr, teamsErr, setErr} {
				if !errors.Is(err, ErrMalformed) {
					t.Errorf("error = %v, want ErrMalformed", err)
				}
			}
		})
	}
}

func TestFileStoreReportsUnreadableAndUnwritablePaths(t *testing.T) {
	ctx := context.Background()
	blocker := filepath.Join(t.TempDir(), "blocker")
	writeFile(t, blocker, "")
	s := NewFileStore(filepath.Join(blocker, "omaslack", "credentials"))
	if _, err := s.Get(ctx, team(t, "T1A")); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Get error = %v, want a read failure", err)
	}
	dirStore := NewFileStore(t.TempDir())
	if err := dirStore.Set(ctx, team(t, "T1A"), DesktopSession{}); err == nil {
		t.Error("Set over a directory succeeded")
	}
	if err := dirStore.Delete(ctx, team(t, "T1A")); err == nil {
		t.Error("Delete over a directory succeeded")
	}
}

func TestFileStoreRejectsCredentialsOfNoKind(t *testing.T) {
	s, path := newTestFileStore(t)
	if err := s.Set(context.Background(), team(t, "T1A"), nil); !errors.Is(err, ErrMalformed) {
		t.Errorf("Set(nil) error = %v, want ErrMalformed", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Set(nil) left a file: %v", err)
	}
}

func TestDefaultFilePathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	t.Setenv("HOME", "/home/someone")
	if got, err := DefaultFilePath(); err != nil || got != "/xdg/omaslack/credentials" {
		t.Errorf("DefaultFilePath() = %q, %v", got, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	if got, err := DefaultFilePath(); err != nil || got != "/home/someone/.config/omaslack/credentials" {
		t.Errorf("DefaultFilePath() = %q, %v", got, err)
	}
	t.Setenv("HOME", "")
	if got, err := DefaultFilePath(); err == nil {
		t.Errorf("DefaultFilePath() = %q with no HOME, want an error", got)
	}
}

func mustSet(t *testing.T, s Store, id domain.TeamID, c Credentials) {
	t.Helper()
	if err := s.Set(context.Background(), id, c); err != nil {
		t.Fatalf("Set(%s): %v", id, err)
	}
}

func assertGet(t *testing.T, s Store, id domain.TeamID, want Credentials) {
	t.Helper()
	got, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	if got != want {
		t.Errorf("Get(%s) = %v, want %v", id, got, want)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != want {
		t.Errorf("%s mode = %v, want %v", path, info.Mode(), want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

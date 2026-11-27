package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsInNameOrder(t *testing.T) {
	got := mustLoadMigrations(fstest.MapFS{
		"migrations/0002_b.sql": {Data: []byte("two")},
		"migrations/0001_a.sql": {Data: []byte("one")},
	})
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("migrations = %q, want [one two]", got)
	}
}

func TestLoadMigrationsPanicsWithoutDirectory(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic for a missing migrations directory")
		}
	}()
	mustLoadMigrations(fstest.MapFS{})
}

// A list stored by a build that did not keep the other person of a DM
// reads back with that person unknown.
func TestAListStoredBeforeDMUsersReadsWithoutThem(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "T123.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := slices.Concat(migrations[:4], []string{"PRAGMA user_version = 4", `INSERT INTO conversations (conv_id, kind) VALUES ('D01', 'im')`})
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.Conversations(ctx)
	if err != nil || len(got) != 1 || got[0].ID.String() != "D01" || !got[0].User.IsZero() {
		t.Errorf("Conversations = %+v, %v; want the DM, with no user", got, err)
	}
}

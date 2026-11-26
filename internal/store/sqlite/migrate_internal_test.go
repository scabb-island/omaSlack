package sqlite

import (
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

package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/store/sqlite"
)

// openStore opens a fresh store in a test-only directory.
func openStore(t *testing.T) store.Store {
	t.Helper()
	return openAt(t, filepath.Join(t.TempDir(), "T123.db"))
}

func openAt(t *testing.T, path string) store.Store {
	t.Helper()
	s, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// rawDB opens the database file behind the store's back.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// rawQuery reads one value from the database file behind the store's back.
func rawQuery(t *testing.T, path, query string) string {
	t.Helper()
	var v string
	if err := rawDB(t, path).QueryRowContext(context.Background(), query).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

// rawExec writes to the database file behind the store's back.
func rawExec(t *testing.T, path, stmt string) {
	t.Helper()
	db := rawDB(t, path)
	if _, err := db.ExecContext(context.Background(), stmt); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	_ = db.Close()
}

func TestOpenUsesWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "T123.db")
	openAt(t, path)
	if got := rawQuery(t, path, "PRAGMA journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, want wal", got)
	}
}

func TestOpenRecordsSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "T123.db")
	openAt(t, path)
	if got := rawQuery(t, path, "PRAGMA user_version"); got != "5" {
		t.Errorf("user_version = %s, want 5", got)
	}
}

func TestReopenKeepsDataAndVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "T123.db")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDraft(ctx, conv(t, "C01"), zeroTS, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := openAt(t, path).Draft(ctx, conv(t, "C01"), zeroTS)
	if err != nil || got != "hello" {
		t.Errorf("Draft after reopen = %q, %v; want hello", got, err)
	}
	if v := rawQuery(t, path, "PRAGMA user_version"); v != "5" {
		t.Errorf("user_version after reopen = %s, want 5", v)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "T123.db")
	rawExec(t, path, "PRAGMA user_version = 99")

	_, err := sqlite.Open(context.Background(), path)
	if !errors.Is(err, sqlite.ErrSchemaTooNew) {
		t.Errorf("Open = %v, want ErrSchemaTooNew", err)
	}
}

func TestFailedMigrationLeavesVersionUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "T123.db")
	// A clashing table makes the first migration fail halfway, after it
	// already created messages.
	rawExec(t, path, "CREATE TABLE drafts (x)")

	if _, err := sqlite.Open(context.Background(), path); err == nil {
		t.Fatal("Open succeeded over a clashing schema")
	}
	if v := rawQuery(t, path, "PRAGMA user_version"); v != "0" {
		t.Errorf("user_version = %s, want 0", v)
	}
	if n := rawQuery(t, path, "SELECT count(*) FROM sqlite_master WHERE name = 'messages'"); n != "0" {
		t.Errorf("messages table exists after a failed migration")
	}
}

func TestOpenRejectsQuestionMarkInPath(t *testing.T) {
	dir := t.TempDir()
	if _, err := sqlite.Open(context.Background(), filepath.Join(dir, "a?b.db")); err == nil {
		t.Error("Open accepted a path with '?'")
	}
}

func TestOpenFailsOnUnusablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "T123.db")
	if _, err := sqlite.Open(context.Background(), path); err == nil {
		t.Error("Open in a missing directory succeeded")
	}
}

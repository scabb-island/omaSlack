package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
)

// ErrSchemaTooNew reports a database written by a newer omaslack, whose
// schema this build does not know. The daemon must not touch it.
var ErrSchemaTooNew = errors.New("database schema is newer than this omaslack")

// migrationFS holds the schema migrations. They are applied in file name
// order, and the Nth file brings the schema to version N, which is kept in
// SQLite's own PRAGMA user_version. A migration is never edited once it is
// released; schema changes go in a new file.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

var migrations = mustLoadMigrations(migrationFS)

// mustLoadMigrations reads every migration in order. It only fails when
// the embedded files are broken, which is a build defect, so it panics.
func mustLoadMigrations(fsys fs.FS) []string {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		panic(fmt.Sprintf("store/sqlite: reading migrations: %v", err))
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		b, err := fs.ReadFile(fsys, path.Join("migrations", e.Name()))
		if err != nil {
			panic(fmt.Sprintf("store/sqlite: reading migration %s: %v", e.Name(), err))
		}
		out[i] = string(b)
	}
	return out
}

// migrate brings db to the latest schema. Every pending migration runs in
// one write transaction that also reads the current version, so two
// processes opening the same file cannot both apply a migration, and a
// crash halfway leaves the previous version intact. An up-to-date database
// is left untouched.
func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store/sqlite: begin migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	version, err := schemaVersion(ctx, tx)
	if err != nil {
		return err
	}
	if version == len(migrations) {
		return nil
	}
	if err := applyFrom(ctx, tx, version); err != nil {
		return err
	}
	return tx.Commit()
}

// schemaVersion reads the version of the open database and refuses one
// newer than this build.
func schemaVersion(ctx context.Context, tx *sql.Tx) (int, error) {
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return version, fmt.Errorf("store/sqlite: read schema version: %w", err)
	}
	if version > len(migrations) {
		return 0, fmt.Errorf("%w: database is at version %d, this build knows up to %d",
			ErrSchemaTooNew, version, len(migrations))
	}
	return version, nil
}

// applyFrom runs every migration after version and records the new one.
func applyFrom(ctx context.Context, tx *sql.Tx, version int) error {
	for _, m := range migrations[version:] {
		if _, err := tx.ExecContext(ctx, m); err != nil {
			return fmt.Errorf("store/sqlite: migrate from version %d: %w", version, err)
		}
	}
	// PRAGMA takes no bound parameters; the value is an int we computed.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations))); err != nil {
		return fmt.Errorf("store/sqlite: record schema version: %w", err)
	}
	return nil
}

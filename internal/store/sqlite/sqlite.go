// Package sqlite implements store.Store on SQLite with the pure-Go
// modernc.org/sqlite driver, so the daemon builds without cgo.
//
// There is one database file per workspace. It runs in WAL mode, so the
// RPC handlers can read while the sync engine writes, with a busy timeout
// and foreign keys on. Write transactions start with BEGIN IMMEDIATE: a
// writer takes the write lock up front and waits for it (up to the busy
// timeout) instead of failing halfway through a transaction.
//
// The database is outside data: every row read back is parsed again with
// the domain parsers, and a malformed value is reported as an error.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// connParams is applied to every pooled connection.
const connParams = "?_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(1)" +
	"&_pragma=journal_mode(WAL)" +
	"&_txlock=immediate"

// Store is one workspace's SQLite cache. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

var _ store.Store = (*Store)(nil)

// Open opens (creating it if needed) the database at path and migrates it
// to the latest schema. The directory must exist. path must not contain
// '?', which the driver reads as the start of its parameters.
//
// Open fails with ErrSchemaTooNew when the file was written by a newer
// omaslack.
func Open(ctx context.Context, path string) (*Store, error) {
	if strings.Contains(path, "?") {
		return nil, fmt.Errorf("store/sqlite: database path %q must not contain '?'", path)
	}
	db, err := sql.Open("sqlite", path+connParams)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: open %s: %w", path, err)
	}
	if err := migrate(ctx, db); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &Store{db: db}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Draft returns the unsent draft of conv or of one of its threads.
func (s *Store) Draft(ctx context.Context, conv domain.ConversationID, thread domain.TS) (string, error) {
	var text string
	err := s.db.QueryRowContext(ctx,
		`SELECT text FROM drafts WHERE conv_id = ? AND thread_ts = ?`,
		conv.String(), optTS(thread)).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store/sqlite: read draft: %w", err)
	}
	return text, nil
}

// SetDraft stores the draft of conv or of one of its threads; an empty
// text deletes it.
func (s *Store) SetDraft(ctx context.Context, conv domain.ConversationID, thread domain.TS, text string) error {
	if text == "" {
		return s.exec(ctx, "delete draft",
			`DELETE FROM drafts WHERE conv_id = ? AND thread_ts = ?`,
			conv.String(), optTS(thread))
	}
	return s.exec(ctx, "write draft",
		`INSERT INTO drafts (conv_id, thread_ts, text) VALUES (?, ?, ?)
		 ON CONFLICT (conv_id, thread_ts) DO UPDATE SET text = excluded.text`,
		conv.String(), optTS(thread), text)
}

// exec runs one write statement in its own implicit transaction.
func (s *Store) exec(ctx context.Context, what, query string, args ...any) error {
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("store/sqlite: %s: %w", what, err)
	}
	return nil
}

// inTx runs fn in one write transaction and commits it only if fn
// succeeds, so a failure or crash halfway leaves no partial write.
func (s *Store) inTx(ctx context.Context, what string, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store/sqlite: %s: %w", what, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return fmt.Errorf("store/sqlite: %s: %w", what, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store/sqlite: %s: %w", what, err)
	}
	return nil
}

// Not implemented yet.
func (s *Store) Apply(context.Context, domain.Event) error { return errors.New("not implemented") }
func (s *Store) Reactions(context.Context, domain.ConversationID, domain.TS) ([]store.Reaction, error) {
	return nil, errors.New("not implemented")
}
func (s *Store) LastRead(context.Context, domain.ConversationID) (domain.TS, error) {
	return domain.TS{}, errors.New("not implemented")
}

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// execer is what writing one row needs: the database or a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// upsertMessage stores m, creating it if it is new. Two rules keep every
// replay and every out-of-order arrival convergent:
//
//   - A version never replaces a newer one: the row changes only when the
//     incoming edited_ts is not older than the stored one ("" sorts before
//     every timestamp, so an unedited copy never undoes an edit).
//   - The deleted mark is never cleared, so a late create or edit cannot
//     bring a deleted message back.
const upsertMessage = `
INSERT INTO messages (conv_id, ts, thread_ts, author, text, edited_ts)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (conv_id, ts) DO UPDATE SET
    thread_ts = excluded.thread_ts,
    author    = excluded.author,
    text      = excluded.text,
    edited_ts = excluded.edited_ts
WHERE excluded.edited_ts >= messages.edited_ts`

func putMessage(ctx context.Context, ex execer, conv domain.ConversationID, m domain.Message) error {
	_, err := ex.ExecContext(ctx, upsertMessage,
		conv.String(), m.TS.String(), optTS(m.ThreadTS), m.Author.String(), m.Text, optTS(m.Edited))
	return err
}

// PutHistory stores a page of messages in one transaction, so a crash
// leaves either the whole page or none of it.
func (s *Store) PutHistory(ctx context.Context, conv domain.ConversationID, msgs []domain.Message) error {
	return s.inTx(ctx, "store history page", func(tx *sql.Tx) error {
		for _, m := range msgs {
			if err := putMessage(ctx, tx, conv, m); err != nil {
				return err
			}
		}
		return nil
	})
}

// Messages returns up to limit messages of conv older than before (all of
// them when before is zero), newest first. Deleted messages are skipped.
// Thread replies are included: the contract has no separate replies query.
func (s *Store) Messages(ctx context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts, thread_ts, author, text, edited_ts FROM messages
		WHERE conv_id = ?1 AND deleted = 0 AND (?2 = '' OR ts < ?2)
		ORDER BY ts DESC
		LIMIT ?3`,
		conv.String(), optTS(before), limit)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: read messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Message
	for rows.Next() {
		var r messageRow
		if err := rows.Scan(&r.ts, &r.threadTS, &r.author, &r.text, &r.editedTS); err != nil {
			return nil, fmt.Errorf("store/sqlite: read messages: %w", err)
		}
		m, err := r.parse()
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// messageRow is a messages row as stored: outside data until parsed.
type messageRow struct {
	ts, threadTS, author, text, editedTS string
}

func (r messageRow) parse() (domain.Message, error) {
	ts, errTS := domain.ParseTS(r.ts)
	thread, errThread := parseOpt(r.threadTS, domain.ParseTS)
	author, errAuthor := parseOpt(r.author, domain.ParseAuthor)
	edited, errEdited := parseOpt(r.editedTS, domain.ParseTS)
	if err := errors.Join(errTS, errThread, errAuthor, errEdited); err != nil {
		return domain.Message{}, fmt.Errorf("store/sqlite: corrupt message %q: %w", r.ts, err)
	}
	return domain.Message{TS: ts, ThreadTS: thread, Author: author, Text: r.text, Edited: edited}, nil
}

// parseOpt parses an optional stored value: "" is the zero value.
func parseOpt[T any](s string, parse func(string) (T, error)) (T, error) {
	if s == "" {
		var zero T
		return zero, nil
	}
	return parse(s)
}

// optTS stores an optional timestamp: the zero TS becomes "".
func optTS(t domain.TS) string {
	if t.IsZero() {
		return ""
	}
	return t.String()
}

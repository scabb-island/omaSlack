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
//   - A version without a thread or an author keeps the stored ones: an
//     edit's result (chat.update) does not always carry them, and a
//     message never leaves its thread or changes hands.
//   - Likewise, a version without a reply count or latest reply keeps the
//     stored ones: an edit, or a source that does not report replies,
//     says nothing of them.
const upsertMessage = `
INSERT INTO messages (conv_id, ts, thread_ts, author, text, edited_ts, reply_count, latest_reply)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (conv_id, ts) DO UPDATE SET
    thread_ts    = COALESCE(NULLIF(excluded.thread_ts, ''), messages.thread_ts),
    author       = COALESCE(NULLIF(excluded.author, ''), messages.author),
    text         = excluded.text,
    edited_ts    = excluded.edited_ts,
    reply_count  = COALESCE(NULLIF(excluded.reply_count, 0), messages.reply_count),
    latest_reply = COALESCE(NULLIF(excluded.latest_reply, ''), messages.latest_reply)
WHERE excluded.edited_ts >= messages.edited_ts`

// messageColumns are what every messages query reads, in messageRow's
// order.
const messageColumns = `ts, thread_ts, author, text, edited_ts, reply_count, latest_reply`

// putMessage stores m and, when this version replaced the stored one
// and carries files, its files. Run it in a transaction.
func putMessage(ctx context.Context, ex execer, conv domain.ConversationID, m domain.Message) error {
	res, err := ex.ExecContext(ctx, upsertMessage,
		conv.String(), m.TS.String(), optTS(m.ThreadTS), m.Author.String(), m.Text, optTS(m.Edited),
		m.ReplyCount, optTS(m.LatestReply))
	if err != nil || len(m.Files) == 0 {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return err
	}
	return putFiles(ctx, ex, conv, m.TS, m.Files)
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

// Messages returns up to limit top-level messages and thread roots of conv
// older than before (all of them when before is zero), newest first.
// Thread replies and deleted messages are left out.
func (s *Store) Messages(ctx context.Context, conv domain.ConversationID, before domain.TS, limit int) ([]domain.Message, error) {
	return s.queryMessages(ctx, conv, `
		SELECT `+messageColumns+` FROM messages
		WHERE conv_id = ?1 AND deleted = 0 AND (?2 = '' OR ts < ?2)
		  AND (thread_ts = '' OR thread_ts = ts)
		ORDER BY ts DESC
		LIMIT ?3`,
		conv.String(), optTS(before), limit)
}

// Oldest returns the oldest top-level message or thread root of conv that
// is not deleted, the same set Messages pages through.
func (s *Store) Oldest(ctx context.Context, conv domain.ConversationID) (domain.TS, error) {
	var ts string
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(ts), '') FROM messages
		WHERE conv_id = ?1 AND deleted = 0 AND (thread_ts = '' OR thread_ts = ts)`,
		conv.String()).Scan(&ts)
	if err != nil {
		return domain.TS{}, fmt.Errorf("store/sqlite: read oldest message: %w", err)
	}
	return parseOpt(ts, domain.ParseTS)
}

// Replies returns the replies of the thread rooted at thread in conv,
// oldest first, without the root. Deleted replies are left out.
func (s *Store) Replies(ctx context.Context, conv domain.ConversationID, thread domain.TS) ([]domain.Message, error) {
	return s.queryMessages(ctx, conv, `
		SELECT `+messageColumns+` FROM messages
		WHERE conv_id = ?1 AND thread_ts = ?2 AND ts <> ?2 AND deleted = 0
		ORDER BY ts ASC`,
		conv.String(), thread.String())
}

// queryMessages runs a query for messages of conv, parses every row it
// returns and adds each message's files.
func (s *Store) queryMessages(ctx context.Context, conv domain.ConversationID, query string, args ...any) ([]domain.Message, error) {
	msgs, err := s.scanMessages(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return s.withFiles(ctx, conv, msgs)
}

// scanMessages runs a messages query and parses every row it returns.
func (s *Store) scanMessages(ctx context.Context, query string, args ...any) ([]domain.Message, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: read messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Message
	for rows.Next() {
		var r messageRow
		if err := rows.Scan(&r.ts, &r.threadTS, &r.author, &r.text, &r.editedTS, &r.replyCount, &r.latestReply); err != nil {
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
	replyCount                           int
	latestReply                          string
}

func (r messageRow) parse() (domain.Message, error) {
	ts, errTS := domain.ParseTS(r.ts)
	thread, errThread := parseOpt(r.threadTS, domain.ParseTS)
	author, errAuthor := parseOpt(r.author, domain.ParseAuthor)
	edited, errEdited := parseOpt(r.editedTS, domain.ParseTS)
	latest, errLatest := parseOpt(r.latestReply, domain.ParseTS)
	if err := errors.Join(errTS, errThread, errAuthor, errEdited, errLatest); err != nil {
		return domain.Message{}, fmt.Errorf("store/sqlite: corrupt message %q: %w", r.ts, err)
	}
	return domain.Message{
		TS: ts, ThreadTS: thread, Author: author, Text: r.text, Edited: edited,
		ReplyCount: max(r.replyCount, 0), LatestReply: latest,
	}, nil
}

// CountReply counts reply on its cached root, unless the root already
// knows a reply as new (see store.Store.CountReply).
func (s *Store) CountReply(ctx context.Context, conv domain.ConversationID, reply domain.Message) (domain.Message, bool, error) {
	if !reply.IsReply() {
		return domain.Message{}, false, nil
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE messages SET reply_count = reply_count + 1, latest_reply = ?3
		WHERE conv_id = ?1 AND ts = ?2 AND deleted = 0 AND ?3 > latest_reply`,
		conv.String(), reply.ThreadTS.String(), reply.TS.String())
	if err != nil {
		return domain.Message{}, false, fmt.Errorf("store/sqlite: count reply: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return domain.Message{}, false, err
	}
	root, err := s.queryMessages(ctx, conv, `SELECT `+messageColumns+` FROM messages WHERE conv_id = ?1 AND ts = ?2`,
		conv.String(), reply.ThreadTS.String())
	if err != nil || len(root) == 0 {
		return domain.Message{}, false, err
	}
	return root[0], true, nil
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
func optTS(t domain.TS) string { return t.OrEmpty() }

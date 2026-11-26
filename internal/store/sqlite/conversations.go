package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// PutConversations replaces the stored conversation list in one
// transaction, so a reader sees the old list or the new one, never a mix.
// Each entry's read marker moves the stored one forward only.
func (s *Store) PutConversations(ctx context.Context, list []domain.Conversation) error {
	return s.inTx(ctx, "store conversation list", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM conversations`); err != nil {
			return err
		}
		for _, c := range list {
			if err := putConversation(ctx, tx, c); err != nil {
				return err
			}
		}
		return nil
	})
}

// putConversation stores one entry; a later duplicate in the same list
// wins.
func putConversation(ctx context.Context, ex execer, c domain.Conversation) error {
	if _, err := ex.ExecContext(ctx, `
		INSERT INTO conversations (conv_id, kind, name, latest, mentions)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (conv_id) DO UPDATE SET
		    kind = excluded.kind, name = excluded.name,
		    latest = excluded.latest, mentions = excluded.mentions`,
		c.ID.String(), c.Kind.String(), c.Name, optTS(c.Latest), c.Mentions); err != nil {
		return err
	}
	if c.LastRead.IsZero() {
		return nil
	}
	return moveMarker(ctx, ex, c.ID, c.LastRead)
}

// Conversations returns the stored list ordered by conversation ID, with
// each entry's read marker and the newest stored top-level message that
// is not deleted (the same set Messages pages through).
func (s *Store) Conversations(ctx context.Context) ([]domain.Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.conv_id, c.kind, c.name, c.latest, c.mentions,
		       COALESCE(r.last_read, ''),
		       COALESCE((SELECT MAX(m.ts) FROM messages m
		                 WHERE m.conv_id = c.conv_id AND m.deleted = 0
		                   AND (m.thread_ts = '' OR m.thread_ts = m.ts)), '')
		FROM conversations c LEFT JOIN read_markers r ON r.conv_id = c.conv_id
		ORDER BY c.conv_id`)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: read conversation list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Conversation
	for rows.Next() {
		var r conversationRow
		if err := rows.Scan(&r.id, &r.kind, &r.name, &r.latest, &r.mentions, &r.lastRead, &r.newest); err != nil {
			return nil, fmt.Errorf("store/sqlite: read conversation list: %w", err)
		}
		c, err := r.parse()
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// conversationRow is a list entry as stored, joined with its read marker
// and its newest stored message: outside data until parsed.
type conversationRow struct {
	id, kind, name, latest, lastRead, newest string
	mentions                                 int
}

func (r conversationRow) parse() (domain.Conversation, error) {
	id, errID := domain.ParseConversationID(r.id)
	kind, errKind := parseKind(r.kind)
	latest, errLatest := parseOpt(r.latest, domain.ParseTS)
	lastRead, errRead := parseOpt(r.lastRead, domain.ParseTS)
	newest, errNewest := parseOpt(r.newest, domain.ParseTS)
	if err := errors.Join(errID, errKind, errLatest, errRead, errNewest); err != nil {
		return domain.Conversation{}, fmt.Errorf("store/sqlite: corrupt conversation %q: %w", r.id, err)
	}
	return view(domain.Conversation{ID: id, Kind: kind, Name: r.name, Latest: latest, LastRead: lastRead, Mentions: r.mentions}, newest), nil
}

// view is what the daemon knows of c once newest, the newest stored
// message, is taken into account: it is the latest when it is newer than
// the list's, and every mention has been read once the marker reaches the
// latest. While the latest is unknown, the mentions stand: a provider may
// count them without knowing the latest message.
func view(c domain.Conversation, newest domain.TS) domain.Conversation {
	if c.Latest.Before(newest) {
		c.Latest = newest
	}
	if !c.Latest.IsZero() && !c.Unread() {
		c.Mentions = 0
	}
	return c
}

// parseKind reads a conversation kind from its name.
func parseKind(s string) (domain.ConversationKind, error) {
	for k := domain.Channel; k.String() != ""; k++ {
		if k.String() == s {
			return k, nil
		}
	}
	return 0, fmt.Errorf("unknown conversation kind %q", s)
}

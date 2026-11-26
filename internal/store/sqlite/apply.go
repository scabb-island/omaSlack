package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
)

// Apply records one domain event. The event's team is not checked: the
// database already belongs to one workspace, and routing events to it is
// the caller's job.
func (s *Store) Apply(ctx context.Context, ev domain.Event) error {
	switch e := ev.(type) {
	case domain.MessageCreated:
		return s.writeMessage(ctx, e.Conversation, e.Message)
	case domain.MessageUpdated:
		// Slack may edit a message older than anything cached, so an
		// update for an unseen message stores it.
		return s.writeMessage(ctx, e.Conversation, e.Message)
	case domain.MessageDeleted:
		return s.deleteMessage(ctx, e.Conversation, e.TS)
	case domain.ReactionChanged:
		return s.changeReaction(ctx, e)
	case domain.ReadChanged:
		return s.markRead(ctx, e.Conversation, e.LastRead)
	default:
		// Presence, typing and connection state are live-only.
		return nil
	}
}

func (s *Store) writeMessage(ctx context.Context, conv domain.ConversationID, m domain.Message) error {
	if err := putMessage(ctx, s.db, conv, m); err != nil {
		return fmt.Errorf("store/sqlite: write message: %w", err)
	}
	return nil
}

// deleteMessage turns the message into a tombstone, creating one if the
// message was never cached, so a create that arrives late stays hidden.
// Its reactions go in the same transaction.
func (s *Store) deleteMessage(ctx context.Context, conv domain.ConversationID, ts domain.TS) error {
	return s.inTx(ctx, "delete message", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO messages (conv_id, ts, deleted) VALUES (?1, ?2, 1)
			ON CONFLICT (conv_id, ts) DO UPDATE SET deleted = 1`,
			conv.String(), ts.String()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`DELETE FROM reactions WHERE conv_id = ?1 AND ts = ?2`,
			conv.String(), ts.String())
		return err
	})
}

// markRead moves the read marker forward only. Events can arrive out of
// order (a reconnect may replay an older read.changed after a newer one),
// and moving back would mark read messages unread again. The cost: a
// deliberate "mark unread" in another Slack client is not reflected until
// the cache is rebuilt.
func (s *Store) markRead(ctx context.Context, conv domain.ConversationID, ts domain.TS) error {
	return s.exec(ctx, "move read marker", `
		INSERT INTO read_markers (conv_id, last_read) VALUES (?, ?)
		ON CONFLICT (conv_id) DO UPDATE SET last_read = excluded.last_read
		WHERE excluded.last_read > read_markers.last_read`,
		conv.String(), optTS(ts))
}

// LastRead returns the read marker of conv, zero if it was never read.
func (s *Store) LastRead(ctx context.Context, conv domain.ConversationID) (domain.TS, error) {
	raw, err := s.queryString(ctx, "read marker",
		`SELECT last_read FROM read_markers WHERE conv_id = ?`, conv.String())
	if err != nil {
		return domain.TS{}, err
	}
	ts, err := parseOpt(raw, domain.ParseTS)
	if err != nil {
		return domain.TS{}, fmt.Errorf("store/sqlite: corrupt read marker of %s: %w", conv, err)
	}
	return ts, nil
}

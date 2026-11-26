package sqlite

import (
	"context"
	"fmt"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/store"
)

// addReaction records one user's reaction unless the message is already
// deleted, so a reaction and a delete converge in either order. Re-adding
// an existing reaction keeps its row and therefore its place.
const addReaction = `
INSERT OR IGNORE INTO reactions (conv_id, ts, emoji, user_id)
SELECT ?1, ?2, ?3, ?4
WHERE NOT EXISTS (
    SELECT 1 FROM messages WHERE conv_id = ?1 AND ts = ?2 AND deleted = 1
)`

// removeReaction drops one user's reaction. An emoji is just the set of its
// rows, so it disappears with its last user.
const removeReaction = `
DELETE FROM reactions WHERE conv_id = ?1 AND ts = ?2 AND emoji = ?3 AND user_id = ?4`

func (s *Store) changeReaction(ctx context.Context, e domain.ReactionChanged) error {
	query := removeReaction
	if e.Added {
		query = addReaction
	}
	return s.exec(ctx, "change reaction", query,
		e.Conversation.String(), e.TS.String(), e.Emoji, e.User.String())
}

// Reactions returns the reactions on one message: emojis in the order
// their earliest current reaction was added, users in the order they
// reacted.
func (s *Store) Reactions(ctx context.Context, conv domain.ConversationID, ts domain.TS) ([]store.Reaction, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT emoji, user_id FROM reactions
		WHERE conv_id = ? AND ts = ?
		ORDER BY rowid`,
		conv.String(), ts.String())
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: read reactions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []store.Reaction
	for rows.Next() {
		var emoji, rawUser string
		if err := rows.Scan(&emoji, &rawUser); err != nil {
			return nil, fmt.Errorf("store/sqlite: read reactions: %w", err)
		}
		u, err := domain.ParseUserID(rawUser)
		if err != nil {
			return nil, fmt.Errorf("store/sqlite: corrupt reaction on %s/%s: %w", conv, ts, err)
		}
		out = appendReaction(out, emoji, u)
	}
	return out, rows.Err()
}

// appendReaction adds u to emoji's group, starting the group if it is new.
// A message carries a handful of emojis, so a linear scan is enough.
func appendReaction(rs []store.Reaction, emoji string, u domain.UserID) []store.Reaction {
	for i := range rs {
		if rs[i].Emoji == emoji {
			rs[i].Users = append(rs[i].Users, u)
			return rs
		}
	}
	return append(rs, store.Reaction{Emoji: emoji, Users: []domain.UserID{u}})
}

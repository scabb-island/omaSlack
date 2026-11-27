-- conversations.user_id: the other person of a direct message ('' for
-- other kinds, and for lists stored before it was kept).
ALTER TABLE conversations ADD COLUMN user_id TEXT NOT NULL DEFAULT '';

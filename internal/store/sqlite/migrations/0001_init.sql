-- Initial schema of one workspace's cache. There is one database per
-- workspace, so no table carries a team_id column.
--
-- IDs and timestamps are stored as their canonical strings. A Slack
-- timestamp is always "<10 digits>.<6 digits>" (domain.TS.String pads the
-- seconds), so ordering by the text orders by time. An empty string means
-- "none" (no thread, no edit, no author).

-- messages: one row per message. A delete keeps the row as a tombstone
-- (deleted = 1) so that a create or edit arriving after the delete cannot
-- bring the message back; readers skip tombstones.
CREATE TABLE messages (
    conv_id   TEXT    NOT NULL,
    ts        TEXT    NOT NULL,
    thread_ts TEXT    NOT NULL DEFAULT '',
    author    TEXT    NOT NULL DEFAULT '',
    text      TEXT    NOT NULL DEFAULT '',
    edited_ts TEXT    NOT NULL DEFAULT '',
    deleted   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (conv_id, ts)
) WITHOUT ROWID;

-- reactions: one row per (message, emoji, user). The rowid records the
-- order reactions were added: a new row always gets a rowid above every
-- existing one, and re-adding an existing reaction keeps its row.
CREATE TABLE reactions (
    conv_id TEXT NOT NULL,
    ts      TEXT NOT NULL,
    emoji   TEXT NOT NULL,
    user_id TEXT NOT NULL,
    UNIQUE (conv_id, ts, emoji, user_id)
);

-- read_markers: the user's read marker per conversation. It only moves
-- forward (see Store.Apply).
CREATE TABLE read_markers (
    conv_id   TEXT NOT NULL PRIMARY KEY,
    last_read TEXT NOT NULL
) WITHOUT ROWID;

-- drafts: unsent text per conversation (thread_ts = '') or per thread.
CREATE TABLE drafts (
    conv_id   TEXT NOT NULL,
    thread_ts TEXT NOT NULL,
    text      TEXT NOT NULL,
    PRIMARY KEY (conv_id, thread_ts)
) WITHOUT ROWID;

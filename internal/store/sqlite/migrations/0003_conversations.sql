-- conversations: the provider's conversation list, replaced whole by every
-- PutConversations. Read markers are not kept here: they stay in
-- read_markers, which only moves forward. latest is the newest message the
-- list reported ('' when unknown); Conversations also looks at the
-- messages table, so a message that arrived after the list still counts.
CREATE TABLE conversations (
    conv_id  TEXT    NOT NULL PRIMARY KEY,
    kind     TEXT    NOT NULL,
    name     TEXT    NOT NULL DEFAULT '',
    latest   TEXT    NOT NULL DEFAULT '',
    mentions INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;

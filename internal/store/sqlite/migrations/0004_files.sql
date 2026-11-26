-- files: one row per file a cached message carries, with what clients show
-- and the URL the daemon downloads it from. A file shared in two messages
-- has one row; the newest metadata stored wins.
CREATE TABLE files (
    file_id      TEXT    NOT NULL PRIMARY KEY,
    name         TEXT    NOT NULL DEFAULT '',
    title        TEXT    NOT NULL DEFAULT '',
    mimetype     TEXT    NOT NULL DEFAULT '',
    size         INTEGER NOT NULL DEFAULT 0,
    download_url TEXT    NOT NULL DEFAULT ''
) WITHOUT ROWID;

-- message_files: the files each message carries, in Slack's order.
CREATE TABLE message_files (
    conv_id  TEXT    NOT NULL,
    ts       TEXT    NOT NULL,
    position INTEGER NOT NULL,
    file_id  TEXT    NOT NULL,
    PRIMARY KEY (conv_id, ts, position)
) WITHOUT ROWID;

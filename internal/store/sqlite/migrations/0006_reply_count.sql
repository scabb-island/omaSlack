-- A thread root's replies, as Slack reports them (reply_count,
-- latest_reply) and as the engine counts the ones that arrive in real
-- time: how many (0 when none or unknown) and the newest one's ts ('' when
-- unknown). A version of the root that does not report them keeps these.
ALTER TABLE messages ADD COLUMN reply_count  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN latest_reply TEXT    NOT NULL DEFAULT '';

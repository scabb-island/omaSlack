-- Replies looks threads up by conversation and root timestamp.
CREATE INDEX messages_by_thread ON messages (conv_id, thread_ts, ts);

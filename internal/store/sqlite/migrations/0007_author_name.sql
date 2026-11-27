-- messages.author_name: the name Slack puts on a message a bot posted (a
-- legacy integration's username, else the bot's profile name); '' for
-- people and for messages stored before it was kept. A version without
-- one keeps the stored name.
ALTER TABLE messages ADD COLUMN author_name TEXT NOT NULL DEFAULT '';

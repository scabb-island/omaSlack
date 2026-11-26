## ADDED Requirements

### Requirement: UI and TUI parity
Every messaging operation in this capability SHALL be available in both the TUI and the UI (the plugin window). A client SHALL omit an operation only when the provider does not support it.

#### Scenario: An operation in one client shows in the other
- **WHEN** the user reacts to a message from the TUI while the UI window is open on the same conversation
- **THEN** the reaction appears in the window without a reload, through the `reaction.changed` event

### Requirement: Conversation list
Clients SHALL show the channels, DMs and group DMs of the active workspace, grouped by Slack's sections when the provider supports them, with unread marks and mention counters.

#### Scenario: Muted channel
- **WHEN** a channel is muted in Slack and receives messages
- **THEN** it shows as a dimmed unread and does not add to the bar badge

### Requirement: Reading messages
Clients SHALL show messages with author, time, day separators, rendered mrkdwn (bold, italic, strikethrough, code, code blocks, quotes, links), mentions resolved to names, emoji, an edited indicator, thread replies and reactions.

#### Scenario: Message with a mention and a link
- **WHEN** a message contains `<@U123>` and `<https://x.y|text>`
- **THEN** it shows `@DisplayName` and a link labeled "text" that opens with `xdg-open`

### Requirement: Sending, editing and deleting
The user SHALL be able to send multi-line messages, edit and delete their own (deleting asks for confirmation) and reply in a thread, optionally also sending to the channel.

#### Scenario: Deleting an own message
- **WHEN** the user chooses to delete one of their messages and confirms
- **THEN** the message disappears from every client through `message.deleted`

### Requirement: Threads
Clients SHALL open a thread in its own view (a side panel in the TUI, a view or panel in the UI window), mark threads with new replies and offer a "threads I follow" view.

#### Scenario: New reply in a followed thread
- **WHEN** a reply arrives in a thread the user follows
- **THEN** the root message shows the "new" indicator and the thread appears in the threads view

### Requirement: Reactions
The user SHALL be able to add and remove reactions with an emoji picker that lists recently used emoji first, and see who reacted.

#### Scenario: Removing an own reaction
- **WHEN** the user picks a reaction they had already added
- **THEN** the reaction is removed and its counter goes down by one

### Requirement: Files and images
The user SHALL be able to attach files (paths or a clipboard image) with a caption, and open or download received files. Downloads SHALL go through the daemon (`files.fetch`) and clients SHALL only receive the local path.

#### Scenario: Pasting an image from the clipboard
- **WHEN** the user pastes an image into the compose box and sends
- **THEN** the image is uploaded to the conversation with the typed text, as one message

### Requirement: Mentions and autocomplete
The compose box SHALL autocomplete `@user`, `#channel` and `:emoji:`, and translate mentions to Slack's format on send.

#### Scenario: Autocompleting a user
- **WHEN** the user types `@ana` and picks "Ana Pérez" from the autocomplete
- **THEN** the sent message contains `<@ANA_ID>`

### Requirement: Search and jumps
Clients SHALL offer a finder for conversations and people (fuzzy; opens DMs and joins channels) and, when the provider supports it, Slack's message search. A search result SHALL open the conversation at that message.

#### Scenario: Search result
- **WHEN** the user picks a search result from a month ago
- **THEN** the conversation opens positioned on that message, not at the end

### Requirement: Single read state
Marking a conversation as read in any client SHALL update the daemon's Store, be sent to Slack and publish `read.changed` and `unread.summary`, so every client agrees within 1 second.

#### Scenario: Read in the window, check the bar
- **WHEN** the user opens an unread DM in the window
- **THEN** the bar badge drops within 1 second, without waiting for any polling

### Requirement: Shared drafts
Drafts SHALL be stored in the daemon per conversation and thread, and SHALL be available in any client.

#### Scenario: Start in the window, finish in the TUI
- **WHEN** the user writes a draft in the window, closes it and opens the same conversation in the TUI
- **THEN** the TUI compose box shows the same draft

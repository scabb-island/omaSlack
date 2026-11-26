## ADDED Requirements

### Requirement: Centralized desktop notifications
Notifications SHALL be emitted by the daemon, once per message, even when several clients are open. They SHALL go through the desktop notification service (Freedesktop, served on Omarchy by `omarchy.notifications`).

#### Scenario: TUI and window both open
- **WHEN** a DM arrives with the TUI and the UI window open
- **THEN** a single notification is shown

### Requirement: Notification rules
By default, DMs, group DMs, mentions (direct and of the user's groups) and replies in threads the user follows SHALL notify. The user SHALL be able to add keywords and channels that notify on everything, and to mute channels. Messages the user sends and anything already pending when the daemon started SHALL NOT notify.

#### Scenario: Keyword
- **WHEN** the user configured the keyword "deploy" and a channel message contains it
- **THEN** a notification is emitted even if the channel is not in the notify-on-everything list

### Requirement: Suppression when the conversation is in view
The daemon SHALL skip the notification when a client reports that the conversation is open and focused.

#### Scenario: Conversation open in the focused window
- **WHEN** a message arrives in the conversation open in the window and the window has focus
- **THEN** no notification is emitted

### Requirement: Notification click
Clicking a notification SHALL open the conversation (or thread) in the preferred client: the UI window if the plugin is loaded, otherwise the TUI through `omarchy-launch-or-focus-tui`. The preference SHALL be configurable.

#### Scenario: UI preference with the plugin loaded
- **WHEN** the user clicks a DM notification and their preference is UI
- **THEN** the plugin window opens on that DM, through the `ui.focus` event

### Requirement: Do not disturb
The user SHALL be able to turn on Slack's Do Not Disturb for a period (30 min, 1 h, 2 h, until tomorrow) and end it, from the UI (bar or panel) and from the TUI. While it is on, the daemon SHALL NOT emit notifications and the bar SHALL show it.

#### Scenario: Snooze from the bar
- **WHEN** the user turns on Do Not Disturb for 1 hour from the panel
- **THEN** Slack is in DND for 1 hour, the bar icon shows the muted indicator and the TUI shows the same state

### Requirement: Presence
The user SHALL be able to set their presence to automatic or away from both clients, and clients SHALL show people's presence in DMs when the provider supports it.

#### Scenario: Going away from the TUI
- **WHEN** the user runs the `:away` command in the TUI
- **THEN** their Slack presence becomes away and the UI reflects it

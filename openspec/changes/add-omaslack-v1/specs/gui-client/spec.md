## ADDED Requirements

### Requirement: Single Service as the daemon client
The plugin SHALL have a `Service.qml` (`kinds: service`) that holds the only connection to the daemon and exposes shared models. `BarWidget`, `Panel` and the window SHALL read from that Service and SHALL NOT open connections of their own.

#### Scenario: Bar and window agree
- **WHEN** the window marks a conversation as read
- **THEN** the bar badge updates from the same event, with no polling

### Requirement: No credentials or network in QML
QML code MUST NOT read credentials or make network requests to Slack. Avatars, images and files SHALL be requested from the daemon, which returns local paths.

#### Scenario: Plugin review
- **WHEN** the QML code is searched for `slack.com` URLs or keyring access
- **THEN** there is none

### Requirement: Bar icon with badges
`BarWidget.qml` SHALL show an icon that follows the bar's color, with one badge for DMs and mentions aimed at the user and another for channels and threads. It SHALL also show the connection state (connected, reconnecting, error, `auth_expired`) and DND, and a tooltip with a per-workspace summary.

#### Scenario: Daemon down
- **WHEN** the daemon does not answer
- **THEN** the icon dims and the tooltip says how to start it

#### Scenario: Clicks
- **WHEN** the user left-clicks the icon
- **THEN** the quick-view Panel opens. A right click, or picking an item in the panel, opens the full window

### Requirement: Quick-view panel
`Panel.qml` SHALL list conversations with unread messages (DMs and mentions first) with a preview, and SHALL allow a quick reply, marking as read, turning on DND and opening the conversation in the window or the TUI.

#### Scenario: Quick reply
- **WHEN** the user types a reply in the panel and presses Enter
- **THEN** the message is sent and the conversation is marked as read

### Requirement: Full window
The plugin SHALL offer a window (a Quickshell `FloatingWindow` with a fixed title for Hyprland rules) with a conversation and workspace sidebar, messages, threads, compose, reactions, attachments, search and finder. It SHALL cover every operation of the `messaging` capability.

#### Scenario: Opening through IPC
- **WHEN** `omarchy-shell shell toggle ar.scabb.omaslack` runs
- **THEN** the window shows if it was hidden and hides if it was visible

### Requirement: Omarchy styling
The whole UI SHALL take colors, fonts and spacing only from the shell's `Style` singleton, with no hardcoded colors, and SHALL look right with light and dark themes.

#### Scenario: Light theme
- **WHEN** the user switches to a light Omarchy theme
- **THEN** the bar, panel and window change without restarting the shell and keep readable contrast

### Requirement: Full keyboard use
The panel and the window SHALL be usable without a mouse: moving between conversations and messages, opening threads, reacting, searching, typing and closing with `Esc`.

#### Scenario: Replying in a thread without a mouse
- **WHEN** the user selects a message with the arrow keys, opens its thread, types and sends
- **THEN** the reply is posted in the thread without the mouse

### Requirement: Long histories without degrading the shell
The message list SHALL be virtualized (`ListView`) and paginated. mrkdwn parsing SHALL happen in the daemon, which hands QML ready rich text, so the shell's thread is not blocked.

#### Scenario: Channel with a long history
- **WHEN** the user scrolls through 2,000 messages of a channel
- **THEN** the shell stays responsive (the bar and other surfaces do not stall)

### Requirement: Settings from the manifest
Widget options (badges, what counts as unread, preferred client to open, hiding the icon when nothing is pending) SHALL be declared in `barWidget.schema[]` with values in `barWidget.defaults`, so they can be edited from the shell settings.

#### Scenario: Hide when nothing is pending
- **WHEN** "hide when there is nothing unread" is on and there is nothing unread
- **THEN** the icon takes no space in the bar

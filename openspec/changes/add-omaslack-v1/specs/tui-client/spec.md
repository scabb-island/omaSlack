## ADDED Requirements

### Requirement: Terminal client on the daemon
`omaslack tui` SHALL be a client of the daemon, with no direct connection to Slack and no credentials of its own, and SHALL implement every operation of the `messaging` capability.

#### Scenario: No credentials in the TUI
- **WHEN** the `omaslack tui` process is inspected
- **THEN** it holds no credentials file open and no network connections to Slack, only the daemon socket

### Requirement: Panel layout
The TUI SHALL show a workspace rail, a conversation sidebar, the message pane, an optional thread pane, the compose box and a status bar with the mode, conversation, workspace and connection state. The sidebar SHALL be hideable and resizable.

#### Scenario: Narrow terminal
- **WHEN** the terminal is narrower than 80 columns
- **THEN** the sidebar hides automatically and can be shown with a shortcut

### Requirement: Vim-like modes and keys
The TUI SHALL have NORMAL, INSERT and COMMAND modes, with `j/k` to move, `h/l`/`Tab` to switch panes, `i` to type, `Esc` to go back, `1`–`9` to switch workspaces and `Ctrl+k`/`Ctrl+t` for the finder. Keys SHALL be documented in built-in help (`?`).

#### Scenario: Switching workspace
- **WHEN** the user presses `2` in NORMAL mode
- **THEN** the TUI switches to the second workspace in the rail

### Requirement: Omarchy theme with live reload
The TUI SHALL read its theme from `~/.local/state/omarchy/current/theme/omaslack.toml` (rendered from the Omarchy template). If it does not exist, it SHALL read the active theme's `colors.toml`. On `SIGUSR1` it SHALL reload the theme without restarting.

#### Scenario: Theme change
- **WHEN** the user changes the Omarchy theme with the TUI open
- **THEN** the hook sends `SIGUSR1` and the TUI redraws with the new colors without losing state

### Requirement: Images in the terminal
The TUI SHALL show images inline with the kitty graphics protocol or sixel, depending on what the terminal supports, and SHALL fall back to half-block characters. It SHALL be able to open an image at full size in an external viewer.

#### Scenario: Terminal without graphics
- **WHEN** the TUI runs in Alacritty
- **THEN** images render as half-block and can be opened in the external viewer with a shortcut

### Requirement: Omarchy-style launch
The TUI SHALL be launchable with `omarchy-launch-or-focus-tui omaslack tui` and with a fixed window class (`org.omarchy.omaslack`), so Hyprland rules and shortcuts can target it.

#### Scenario: Shortcut with the TUI already open
- **WHEN** the TUI is already open and the user presses the shortcut
- **THEN** the existing window is focused instead of opening another

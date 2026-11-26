## ADDED Requirements

### Requirement: Valid plugin manifest
The plugin SHALL have a `manifest.json` with `schemaVersion: 1`, `id` = `ar.scabb.omaslack` (the reversed `scabb.ar` domain; permanent in the marketplace and never in the `omarchy.*` namespace), `name`, `version`, `author`, `license`, `description`, `kinds: ["service","bar-widget","panel"]`, `entryPoints` and the `barWidget` block. It SHALL pass `omarchy plugin validate` and SHALL NOT contain symlinks.

#### Scenario: Local validation
- **WHEN** `make local` runs
- **THEN** `omarchy plugin validate plugin` and `qmllint` finish without errors

### Requirement: Documented install and removal
The plugin README SHALL explain how to install the binary (AUR) and the plugin (`omarchy plugin add <git-url> --enable`), how to place it in the bar and how to remove it completely: plugin, service, template, hook and data.

#### Scenario: Uninstalling
- **WHEN** the user follows the removal steps
- **THEN** no systemd unit, hook, template or keyring credentials are left behind

### Requirement: Theme template and hook
Installation SHALL put `~/.config/omarchy/themed/omaslack.toml.tpl` (with Omarchy's color variables) and the `~/.config/omarchy/hooks/theme-set.d/omaslack` hook, which sends `SIGUSR1` to open TUIs, in place. It SHALL NOT overwrite files the user has modified.

#### Scenario: Template edited by the user
- **WHEN** an `omaslack.toml.tpl` with user changes already exists
- **THEN** installation does not overwrite it and reports that a newer version is available

### Requirement: Shortcuts and launcher
The project SHALL include a `.desktop` entry for the TUI (visible in the Omarchy launcher) and SHALL document suggested Hyprland shortcuts: toggling the window (`omarchy-shell shell toggle ar.scabb.omaslack`) and opening or focusing the TUI.

#### Scenario: Searching the launcher
- **WHEN** the user searches for "omaslack" in the launcher
- **THEN** the entry that opens or focuses the TUI appears

### Requirement: Diagnostics
`omaslack doctor` SHALL check and report: daemon and protocol version, service state, socket permissions, where credentials live, the provider per workspace (with the terms-of-service warning for `desktop-session`), whether the template and hook are present, and whether the plugin is installed and enabled (`omarchy plugin list --json`).

#### Scenario: Missing hook
- **WHEN** the `theme-set.d/omaslack` hook does not exist
- **THEN** `doctor` flags it as a warning and prints the command to reinstall it

### Requirement: Coexistence with other Slack clients
The documentation SHALL explain how to coexist with the official app (hide its tray icon, avoid duplicate notifications) and with other Slack plugins from the marketplace. The plugin SHALL NOT change other plugins' configuration.

#### Scenario: Official app running
- **WHEN** the official Slack app is also running
- **THEN** the README explains how to turn off its notifications to avoid receiving them twice

### Requirement: Distribution
The binary SHALL be published with signed releases (goreleaser) and the AUR package `omaslack-bin`, as specified in the `aur-packaging` capability. The plugin SHALL be submitted to the Omarchy marketplace following `SUBMISSION.md`.

#### Scenario: Clean install
- **WHEN** a new user installs `omaslack-bin` and then the plugin
- **THEN** after `omaslack auth add` they have the bar, panel, window and TUI working against the same daemon

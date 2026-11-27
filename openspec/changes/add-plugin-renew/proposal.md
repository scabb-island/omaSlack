# Change: add-plugin-renew — a one-click renew in the QML plugin

## Why

The CLI (`omaslack auth refresh`) and the TUI (Ctrl+R) can renew an expired
desktop-session in place. The QML plugin's window should offer the same, so
a user who lives in the UI recovers from `auth_expired` with one click
instead of dropping to a terminal.

## What Changes

- **ADDED** to `slack-connection`: when the open workspace is `auth_expired`
  with the `desktop-session` provider, the plugin window's status line
  offers a one-click "Session expired — click to renew" that runs
  `omaslack auth refresh <team>`. On success the daemon reconnects and the
  expired state clears. No tokens pass through QML: the subprocess reads the
  browser or desktop app itself.

## Impact

- **Affected specs:** `slack-connection`.
- **Code:** `plugin/Service.qml` (`renew(team)`), `plugin/Window.qml` (the
  clickable status line when `canRenew`), `plugin/lib/badges.js` (the bar
  tooltip hint mentions renewing).
- **Users:** recover from `auth_expired` from the window.

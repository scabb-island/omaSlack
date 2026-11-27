# Change: add-tui-renew — a one-step renew action in the TUI

## Why

`omaslack auth refresh` renews an expired desktop-session from the CLI
(change add-session-refresh). A TUI user who sees a workspace go
`auth_expired` should not have to drop to a shell: the TUI can run the same
refresh in place.

## What Changes

- **ADDED** to `slack-connection`: the TUI offers a one-step renew action
  (Ctrl+R) on the active workspace when it is `auth_expired` with the
  `desktop-session` provider. It runs `omaslack auth refresh <team>`; on
  success the daemon reconnects and the error state clears. The status bar
  hints "(C-r to renew)" while a desktop session is expired.

Out of scope: the same action in the QML plugin (a follow-up change).

## Impact

- **Affected specs:** `slack-connection`.
- **Code:** `internal/tui/app` (a Renew key, the action, a status hint, and
  an injectable refresh seam).
- **Users:** recover from `auth_expired` without leaving the TUI.

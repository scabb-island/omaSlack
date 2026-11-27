# Change: add-session-refresh — renew an expired desktop-session without re-adding it

## Why

A `desktop-session` credential is a snapshot of a session the user already
holds. When Slack rejects it (`auth_expired`), the session the browser or
desktop app holds is often still valid (only the stored copy went stale),
or the user only needs to open Slack there again to refresh it. Today the
only remedy is to run the full `omaslack auth add` flow again (consent,
pick the source, pick the workspace) and then reload the daemon by hand.
That is heavy for what is usually a one-step renewal.

## What Changes

- **ADDED** to `slack-connection`: `omaslack auth refresh [team-id]`
  re-reads the session of a `desktop-session` workspace from the same local
  source it came from (the Slack desktop app or a browser profile),
  validates it with `auth.test`, updates the keyring and asks the daemon to
  reconnect — no consent prompt and no source/workspace menu. With no
  argument it refreshes every `desktop-session` workspace the daemon reports
  as `auth_expired`. It never touches `internal-app` workspaces, which have
  their own tokens.
- **ADDED** to `daemon-api`: a `workspaces.reload` method so a client can
  ask the daemon to read the keyring again (the same effect as SIGHUP),
  so `auth add`/`auth refresh` take effect without `systemctl reload`.
Out of scope here (a follow-up change adds them): a one-step "renew
session" action in the TUI and the QML plugin on an `auth_expired`
workspace; refreshing `internal-app` (it does not expire this way);
automatic background re-reading in the daemon.

## Impact

- **Affected specs:** `slack-connection` (refresh command, TUI/plugin
  action) and `daemon-api` (`workspaces.reload`).
- **Code:** `internal/cli` (`auth refresh`), `internal/provider/desktopsession/auth`
  (re-read a source by team), `internal/api` + `internal/daemon`
  (`workspaces.reload`).
- **Users:** a one-step recovery from `auth_expired` for browser/desktop
  sessions, from the CLI, the TUI or the plugin.

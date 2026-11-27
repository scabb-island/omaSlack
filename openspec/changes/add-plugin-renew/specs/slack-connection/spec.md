## ADDED Requirements

### Requirement: Renew action in the plugin

When the open workspace is `auth_expired` with the `desktop-session`
provider, the QML plugin window SHALL offer a one-click renew that runs the
same refresh as `omaslack auth refresh` and lets the workspace reconnect. No
credential SHALL pass through QML: the subprocess reads the local source
itself.

#### Scenario: Renew an expired desktop session from the window

- **WHEN** the open workspace is `auth_expired` with the `desktop-session` provider
- **THEN** the window's status line offers "Session expired — click to renew", and clicking it runs the refresh for that workspace; on success the daemon reconnects and the expired state clears

#### Scenario: No renew prompt otherwise

- **WHEN** the open workspace is connected, or uses `internal-app`
- **THEN** the window shows no renew prompt

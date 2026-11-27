## ADDED Requirements

### Requirement: Renew action in the TUI

The TUI SHALL offer a one-step renew action on the active workspace when it
is `auth_expired` with the `desktop-session` provider, which runs the same
refresh as `omaslack auth refresh` and lets the workspace reconnect. For any
other state the action reports that there is nothing to renew.

#### Scenario: Renew an expired desktop session from the TUI

- **WHEN** the active workspace is `auth_expired` with the `desktop-session` provider and the user presses the renew key
- **THEN** the TUI runs the refresh for that workspace and, on success, the daemon reconnects it and the error state clears

#### Scenario: Nothing to renew

- **WHEN** the active workspace is connected, or uses `internal-app`, and the user presses the renew key
- **THEN** the TUI does not run a refresh and says there is nothing to renew

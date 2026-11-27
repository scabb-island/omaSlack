## ADDED Requirements

### Requirement: One-step session refresh

`omaslack auth refresh` SHALL renew a `desktop-session` workspace by
re-reading its session from the same kind of local source it came from (the
Slack desktop app or a browser profile), validating it with `auth.test`,
updating the keyring and asking the daemon to reconnect. It SHALL NOT prompt
for consent again or show a source or workspace menu, and it SHALL NOT touch
`internal-app` workspaces.

#### Scenario: Refresh one expired workspace

- **WHEN** a `desktop-session` workspace is `auth_expired` and its source still holds a valid session, and the user runs `omaslack auth refresh <team-id>`
- **THEN** the session is re-read from that source, validated with `auth.test`, written to the keyring, and the daemon reconnects the workspace without `systemctl reload`

#### Scenario: Refresh every expired workspace

- **WHEN** the user runs `omaslack auth refresh` with no argument
- **THEN** every `desktop-session` workspace the daemon reports as `auth_expired` is refreshed, and `internal-app` workspaces are left untouched

#### Scenario: The source no longer holds a valid session

- **WHEN** the workspace's source has no valid session (signed out there too)
- **THEN** nothing is stored, the workspace stays `auth_expired`, and the CLI tells the user to sign in to Slack again in that source and run `omaslack auth refresh` again

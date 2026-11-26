## ADDED Requirements

### Requirement: Interchangeable connection providers
The daemon SHALL talk to Slack only through the `Provider` interface. v1 SHALL ship two implementations: `desktop-session` and `internal-app`. Each workspace SHALL have exactly one provider assigned.

#### Scenario: Workspace with the internal-app provider
- **WHEN** the user runs `omaslack auth add --provider internal-app` and enters a valid xoxp user token and xapp app token
- **THEN** the workspace is registered with `provider = internal-app` and the daemon connects through the Web API and Socket Mode

#### Scenario: Workspace with the desktop-session provider
- **WHEN** the user runs `omaslack auth add --provider desktop-session` and the Slack desktop app is signed in
- **THEN** the CLI lists the workspaces found in the desktop session, the user picks which to add, and each one is registered with `provider = desktop-session`

### Requirement: Explicit consent for the unofficial protocol
The `desktop-session` provider MUST require the user to accept a warning explaining that it uses an unofficial protocol that may violate Slack's terms of service. Without that acceptance, the workspace MUST NOT be registered.

#### Scenario: The user declines the warning
- **WHEN** the user picks `desktop-session` and answers "no" to the terms-of-service warning
- **THEN** no credential is stored and the CLI suggests the `internal-app` provider with a link to its guide

### Requirement: Browser session login as an alternative
The `desktop-session` provider SHALL accept, as an alternative, a cURL command copied from the browser's DevTools (a request to `/api/` on app.slack.com) and extract the xoxc token and xoxd cookie from it.

#### Scenario: No desktop app installed
- **WHEN** no desktop app session is found and the user pastes a valid cURL command
- **THEN** the workspace is registered with the credentials extracted from the cURL command

### Requirement: Provider capability advertisement
Each provider SHALL declare a `Capabilities` set (for example `realtime`, `search`, `files`, `presenceWrite`, `typing`, `customEmoji`). The daemon SHALL expose it over RPC so the TUI and the UI hide or disable what the provider does not support.

#### Scenario: Unsupported feature
- **WHEN** a provider declares `typing = false`
- **THEN** neither the TUI nor the UI shows "is typing" indicators for that workspace

### Requirement: Credentials in the keyring
Credentials SHALL be stored in the Secret Service with the attribute `service=omaslack`. Without a Secret Service, a file with 0600 permissions at `$XDG_CONFIG_HOME/omaslack/credentials` SHALL be used, and `omaslack doctor` SHALL show a warning. No RPC method MUST ever return credentials.

#### Scenario: No Secret Service
- **WHEN** there is no Secret Service provider on D-Bus
- **THEN** credentials are stored in the 0600 file and `omaslack doctor` flags the "credentials in a file" warning

#### Scenario: A client asks for workspace data
- **WHEN** a client calls `workspaces.list`
- **THEN** the response includes name, team_id, provider and state, but no token or cookie

### Requirement: Parallel multi-workspace
The daemon SHALL keep every registered workspace connected in parallel, each with its own provider, connection state and unread counters.

#### Scenario: Two workspaces with different providers
- **WHEN** one workspace uses `internal-app` and another uses `desktop-session`
- **THEN** both receive messages in real time and `unread.summary` aggregates the counters of both

### Requirement: Expired credentials
When Slack rejects a credential (for example `invalid_auth` or `token_revoked`), the daemon SHALL mark the workspace as `auth_expired`, stop retrying and publish `connection.state` so clients ask the user to authenticate again.

#### Scenario: Revoked token
- **WHEN** Slack answers `token_revoked` for a workspace
- **THEN** the workspace moves to `auth_expired`, the bar shows the icon in its error state and the TUI shows "authenticate again: omaslack auth add"

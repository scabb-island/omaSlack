## ADDED Requirements

### Requirement: Browser session read automatically

The `desktop-session` provider SHALL read the Slack web session
automatically from an installed Chromium-based browser (Brave, Google
Chrome, Chromium or Microsoft Edge), the same way it reads the Slack
desktop app, and SHALL confirm the session with Slack's `auth.test` before
storing it. It SHALL read the browser profile's files only through
temporary copies, so it is safe to run while the browser is open.

#### Scenario: Add a workspace from a browser session

- **WHEN** the user runs `omaslack auth add --provider desktop-session`, accepts the consent warning, and a detected Chromium browser is signed in to Slack
- **THEN** the CLI offers the browser as a session source alongside the Slack desktop app, reads the `xoxc` token from the profile's `localConfig_v2` and the `d` cookie plus the other `.slack.com` cookies from the profile's `Cookies` database (decrypted with the browser's "Safe Storage" key, whose keyring `application` attribute is the browser's lowercase name such as `brave` or `chrome`), and registers the workspace with `provider = desktop-session`, its `auth.test` name and URL, and the whole cookie set
- **AND** because a browser profile has no desktop-app workspace list, the team comes from the token entries and the name and URL come from `auth.test`

#### Scenario: No desktop app, a browser is signed in

- **WHEN** the Slack desktop app is not installed but a Chromium browser holds a Slack session
- **THEN** the user can add the workspace from the browser without pasting a cURL command, and the manual cURL paste remains available as a fallback

#### Scenario: Browser profile is locked while the browser runs

- **WHEN** the browser is open and holds its `Local Storage` and `Cookies` databases locked, and its `Cookies` database is at either `Network/Cookies` or `Cookies` within the profile
- **THEN** the provider still reads the session from whichever location holds it, because it works on temporary copies of the profile's files and never locks or writes the browser's own profile

#### Scenario: A browser session Slack no longer accepts

- **WHEN** the session read from the browser is expired and `auth.test` rejects it
- **THEN** no credential is stored and the CLI explains that some workspaces bind sessions to Slack's own client and points the user to the `internal-app` provider and its guide

#### Scenario: Doctor reports the available session sources

- **WHEN** the user runs `omaslack doctor`
- **THEN** the report names which `desktop-session` sources are available: the Slack desktop app and each detected Chromium browser signed in to Slack

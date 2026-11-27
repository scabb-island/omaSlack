## ADDED Requirements

### Requirement: Every browser profile is a session source

The `desktop-session` provider SHALL read the Slack web session of every
profile of each detected Chromium browser, not only the Default profile. A
browser keeps each profile in its own directory (`Default`, `Profile 1`,
…); each profile that holds a Slack session SHALL be offered as its own
source, named after the browser and, for a non-Default profile, the
profile's display name from the browser's `Local State`.

#### Scenario: A workspace in a non-Default profile is offered

- **WHEN** a Chromium browser has a Slack session in a non-Default profile (for example `Profile 1`, shown in the browser as "Primary")
- **THEN** `omaslack auth add --provider desktop-session` and `omaslack doctor` list that profile as a source named after the browser and the profile (for example "Brave (Primary)"), and reading it returns that profile's workspaces

#### Scenario: The Default profile keeps the plain browser name

- **WHEN** a browser's Default profile holds a Slack session
- **THEN** its source is named after the browser alone (for example "Brave"), as before

#### Scenario: Non-profile directories are ignored

- **WHEN** a browser directory contains "System Profile" or "Guest Profile", or a profile with no Local Storage
- **THEN** those are not offered as sources

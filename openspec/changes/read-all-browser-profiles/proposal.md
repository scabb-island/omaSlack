# Change: read-all-browser-profiles — read every browser profile, not only Default

## Why

A Chromium browser can hold several profiles (Default, "Profile 1", …),
each with its own Slack sessions. People keep work and personal Slack in
separate browser profiles. The browser-session reader added in
add-browser-session-login only reads each browser's Default profile, so a
workspace signed in to another profile is invisible to
`omaslack auth add` and `omaslack doctor`.

A live read-only test found exactly this: the A3Mercados workspace
(matbarofex.slack.com) is signed in to a non-Default Brave profile, Slack's
`auth.test` accepts its session, yet `FindBrowsers` never offered it.

## What Changes

- **ADDED** to `slack-connection`: `desktop-session` reads every profile of
  each detected Chromium browser, not only Default. Each profile is offered
  as its own source, named after the browser and the profile's display name
  (from the browser's `Local State`), for example "Brave (Primary)".

## Impact

- **Affected specs:** `slack-connection` (one new requirement).
- **Code:** `internal/provider/desktopsession/auth` (`FindBrowsers` and the
  profile enumeration). The CLI and doctor need no change: they already
  iterate whatever `FindBrowsers` returns.
- **Users:** a workspace signed in to a non-Default browser profile is now
  found and can be added.

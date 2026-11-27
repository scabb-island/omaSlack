# Change: add-browser-session-login — read the Slack web session straight from a Chromium browser

## Why

The `desktop-session` provider lets omaslack reuse a Slack session the user
already holds, with no official Slack app of their own and no internal
Slack app to register. Today it reads that session from two places: the
**Slack desktop app** (automatically) and a **browser** (only when the user
pastes a "Copy as cURL" by hand). Many Omarchy users never install the
Slack desktop app — they use Slack in their browser. For them the only path
is the manual cURL paste, which is fiddly and error-prone.

A browser stores the Slack **web** session in the exact same Chromium
layout the desktop app uses: the `xoxc` token in `localConfig_v2` under
`Local Storage/leveldb`, and the `d` cookie (plus the other `.slack.com`
cookies) in the `Cookies` SQLite database, encrypted with the browser's
own "Safe Storage" key in the Secret Service keyring. omaslack already
reads all of this for the desktop app. Pointing the same machinery at a
browser profile closes the "no official Slack client, no Slack app" goal
for browser users, reusing a session the user created themselves — not
minting one and not imitating the official client.

A spike (`spikes/browser-session`) proved it end to end: it read the
Scabb Island session from Brave and Slack's `auth.test` accepted it.

## What Changes

- **ADDED** to `slack-connection`: the `desktop-session` provider reads the
  Slack web session automatically from an installed Chromium browser
  (Brave, Chrome, Chromium, Microsoft Edge), the same way it reads the
  Slack desktop app. `omaslack auth add --provider desktop-session` lets
  the user choose the session source (desktop app, a detected browser, or
  the manual cURL paste that still exists as a fallback).
- Browser session reading reuses the existing decrypt, LevelDB and cookie
  code. The only new pieces are browser-profile discovery, the per-browser
  keyring label (the `application` attribute is lowercase: `brave`,
  `chrome`, `chromium`), the newer `Network/Cookies` location, and
  workspace discovery without the desktop app's `root-state.json` (teams
  come from the tokens; `auth.test` fills in name and URL).
- `omaslack doctor` reports which session sources are available.

Out of scope: Firefox (different, non-Chromium storage — a later change);
anything that mints or refreshes a session, imitates the official client's
native TSAuth/toolbelt OAuth layer, or works around Slack's device checks.

## Impact

- **Affected specs:** `slack-connection` (one new requirement; the existing
  requirements, including the manual cURL paste and the consent warning,
  are unchanged).
- **Code:** `internal/provider/desktopsession/auth` (browser profile
  discovery and per-browser keyring label, reusing the existing readers),
  `internal/cli` (`auth add` source selection), `internal/doctor`.
- **Users:** browser users add a workspace without the Slack desktop app
  and without pasting cURL. The `desktop-session` consent warning still
  applies.
- **Risks:** browser profile layouts vary by version (the `Cookies`
  location moved under `Network/`); a workspace that binds sessions to its
  own client still rejects a reused browser session, so `internal-app`
  stays the fallback for those.

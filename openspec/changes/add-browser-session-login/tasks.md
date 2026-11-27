# Tasks — add-browser-session-login

## Browser profile discovery and reading
- [x] 1.1 `auth`: discover installed Chromium browser profiles (Brave, Chrome, Chromium, Edge) and their default profile directory, with the `Cookies` location at `Network/Cookies` or `Cookies`
- [x] 1.2 `auth`: a `PasswordSource` that reads the browser's "Safe Storage" key from the Secret Service with the browser's lowercase `application` attribute (`brave`, `chrome`, `chromium`), reusing the existing Secret Service / KWallet lookups
- [x] 1.3 `auth`: read a browser session into `[]Workspace` — reuse the existing LevelDB token reader, `Cookies` reader and cookie decrypter; discover the team from the tokens and leave name/URL for `auth.test` (no `root-state.json`)
- [x] 1.4 `auth`: unit tests with a fixture Chromium profile (fake leveldb + Cookies + fake keyring), including the locked-while-open path (temporary copies) and the two `Cookies` locations

## auth add integration
- [x] 2.1 `cli`: `auth add --provider desktop-session` lists session sources — the Slack desktop app and each detected browser — and reads the picked source; the manual cURL paste stays as a fallback
- [x] 2.2 `cli`: a browser session rejected by Slack gives the same `invalid_auth`/bound-session help as the desktop-app and pasted paths
- [x] 2.3 `cli`: tests for the new source selection and the rejection help

## doctor
- [x] 3.1 `doctor`: report the available `desktop-session` sources (desktop app + detected browsers), through injected probes, as OK/WARN
- [x] 3.2 `doctor`: tests

## Close-out
- [ ] 4.1 Live test against Scabb Island from the user's Brave profile; confirm `auth test`/daemon connect and nothing is marked read
- [ ] 4.2 `docs`/`packaging/omarchy/README.md`: note browser as a session source; remove the throwaway `spikes/browser-session`
- [ ] 4.3 `make local` green (check, lint, qml, OpenSpec validate --strict)
